// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messaging

// ZAP-native codec for Conversation envelopes.
//
// Replaces the legacy json/v2 Serialize/DeserializeConversation pair,
// which was UTF-8 unsafe: the membership-CRDT tag was raw hash bytes
// stuffed into a Go string used as a map key, and any JSON round-trip
// either base64-encoded that key (json/v2) or rejected it as bad UTF-8.
//
// Two layers:
//
//  1. A ZAP envelope (magic+version+root) holds three top-level fields:
//       - Kind        u8   @ 0    # conversationKind = 1
//       - HeaderBlob  bytes @ 1   # fixed-width conversation header
//       - BodyBlob    bytes @ 9   # concatenated CRDT entries
//     Variable-tail lengths live in the bytes' (offset, length) pair,
//     so an attacker cannot smuggle non-zero state through unused fields.
//
//  2. HeaderBlob carries the scalar fields packed at fixed offsets in
//     little-endian. BodyBlob carries length-prefixed CRDT entries: each
//     entry is `u32 length || u8 kind || payload`. Entry kinds:
//       - 0x01 = MemberEntry  (active or tombstoned)
//       - 0x02 = ReadMarker
//       - 0x03 = EncryptedKey
//
// Forward-only: there is exactly one wire kind. A future change MUST bump
// `conversationKind` (the discriminator byte at envelope offset 0) and
// add a new decoder branch — no backwards compat aliases.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

const (
	conversationKind uint8 = 1

	// Envelope offsets (the outer ZAP root object).
	offEnvKind   = 0
	offEnvHeader = 1 // bytes (relOff, len) — 8 bytes
	offEnvBody   = 9 // bytes (relOff, len) — 8 bytes
	sizeEnvelope = 17

	// HeaderBlob layout (fixed, 116 bytes — three u32 length fields end at 116).
	offHdrType                 = 0
	offHdrDisappearingMessages = 1
	offHdrKeyRotationEpoch     = 8
	offHdrCreatedAtUnixNs      = 16
	offHdrUpdatedAtUnixNs      = 24
	offHdrMessageTTL           = 32
	offHdrID                   = 40
	offHdrVersion              = 72
	offHdrEncNameLen           = 104
	offHdrEncDescLen           = 108
	offHdrEncAvatarLen         = 112
	sizeHeaderFixed            = 116
	// After the fixed header, the encrypted-name/desc/avatar bytes are
	// appended in order. Lengths are uint32 LE at the offsets above.

	// BodyBlob entry kinds.
	entryKindMember       uint8 = 1
	entryKindReadMarker   uint8 = 2
	entryKindEncryptedKey uint8 = 3

	// MemberEntry payload layout (variable-tail).
	memOffRemoved         = 0
	memOffAddedAtUnixNs   = 8
	memOffRemovedAtUnixNs = 16
	memOffTag             = 24 // 16 bytes (raw OR-Set tag)
	memOffAccountID       = 40
	memOffAddedBy         = 72
	memOffRoleLen         = 104 // u32 — role bytes follow nickname-len's tail
	memOffNicknameLen     = 108
	memPayloadFixed       = 112
	// After payload-fixed: role bytes, then nickname bytes.

	// ReadMarker payload layout (fixed 80 bytes).
	rmOffAccountID       = 0
	rmOffLastReadID      = 32
	rmOffLastReadUnixNs  = 64
	rmOffUpdatedAtUnixNs = 72
	rmPayloadSize        = 80
)

// ErrUnknownConversationKind is returned when the discriminator byte at
// envelope offset 0 does not match a known kind.
var ErrUnknownConversationKind = errors.New("messaging: unknown conversation wire kind")

// ErrCorruptConversation is returned when the wire shape is internally
// inconsistent (entry length out of range, payload too short, etc.).
var ErrCorruptConversation = errors.New("messaging: corrupt conversation blob")

// encodeConversation serializes a Conversation into a ZAP envelope.
func encodeConversation(c *Conversation) []byte {
	header := buildHeader(c)
	body := buildBody(c)

	b := zap.NewBuilder(zap.HeaderSize + sizeEnvelope + len(header) + len(body) + 64)
	ob := b.StartObject(sizeEnvelope)
	ob.SetUint8(offEnvKind, conversationKind)
	ob.SetBytes(offEnvHeader, header)
	ob.SetBytes(offEnvBody, body)
	ob.FinishAsRoot()
	return b.Finish()
}

// decodeConversation parses a ZAP envelope back into a Conversation.
func decodeConversation(data []byte) (*Conversation, error) {
	msg, err := zap.Parse(data)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	if root.Uint8(offEnvKind) != conversationKind {
		return nil, ErrUnknownConversationKind
	}

	header := root.Bytes(offEnvHeader)
	body := root.Bytes(offEnvBody)
	if len(header) < sizeHeaderFixed {
		return nil, fmt.Errorf("%w: header %d < %d", ErrCorruptConversation, len(header), sizeHeaderFixed)
	}

	c := &Conversation{
		Type:                 ConversationType(header[offHdrType]),
		DisappearingMessages: header[offHdrDisappearingMessages] == 1,
		KeyRotationEpoch:     binary.LittleEndian.Uint64(header[offHdrKeyRotationEpoch:]),
		CreatedAt:            time.Unix(0, int64(binary.LittleEndian.Uint64(header[offHdrCreatedAtUnixNs:]))),
		UpdatedAt:            time.Unix(0, int64(binary.LittleEndian.Uint64(header[offHdrUpdatedAtUnixNs:]))),
		MessageTTL:           int64(binary.LittleEndian.Uint64(header[offHdrMessageTTL:])),
	}
	copy(c.ID[:], header[offHdrID:offHdrID+32])
	copy(c.Version[:], header[offHdrVersion:offHdrVersion+32])

	encNameLen := binary.LittleEndian.Uint32(header[offHdrEncNameLen:])
	encDescLen := binary.LittleEndian.Uint32(header[offHdrEncDescLen:])
	encAvatarLen := binary.LittleEndian.Uint32(header[offHdrEncAvatarLen:])
	tailEnd := uint32(sizeHeaderFixed) + encNameLen + encDescLen + encAvatarLen
	if uint32(len(header)) < tailEnd {
		return nil, fmt.Errorf("%w: header tail %d < %d", ErrCorruptConversation, len(header), tailEnd)
	}
	tail := header[sizeHeaderFixed:]
	if encNameLen > 0 {
		c.EncryptedName = append([]byte(nil), tail[:encNameLen]...)
		tail = tail[encNameLen:]
	}
	if encDescLen > 0 {
		c.EncryptedDescription = append([]byte(nil), tail[:encDescLen]...)
		tail = tail[encDescLen:]
	}
	if encAvatarLen > 0 {
		c.EncryptedAvatar = append([]byte(nil), tail[:encAvatarLen]...)
	}

	c.MembersCRDT = NewMembershipCRDT()
	c.ReadMarkersCRDT = NewReadMarkerCRDT()

	if err := parseBody(body, c); err != nil {
		return nil, err
	}
	return c, nil
}

func buildHeader(c *Conversation) []byte {
	encName := c.EncryptedName
	encDesc := c.EncryptedDescription
	encAvatar := c.EncryptedAvatar

	out := make([]byte, sizeHeaderFixed+len(encName)+len(encDesc)+len(encAvatar))
	out[offHdrType] = uint8(c.Type)
	if c.DisappearingMessages {
		out[offHdrDisappearingMessages] = 1
	}
	binary.LittleEndian.PutUint64(out[offHdrKeyRotationEpoch:], c.KeyRotationEpoch)
	binary.LittleEndian.PutUint64(out[offHdrCreatedAtUnixNs:], uint64(c.CreatedAt.UnixNano()))
	binary.LittleEndian.PutUint64(out[offHdrUpdatedAtUnixNs:], uint64(c.UpdatedAt.UnixNano()))
	binary.LittleEndian.PutUint64(out[offHdrMessageTTL:], uint64(c.MessageTTL))
	copy(out[offHdrID:], c.ID[:])
	copy(out[offHdrVersion:], c.Version[:])
	binary.LittleEndian.PutUint32(out[offHdrEncNameLen:], uint32(len(encName)))
	binary.LittleEndian.PutUint32(out[offHdrEncDescLen:], uint32(len(encDesc)))
	binary.LittleEndian.PutUint32(out[offHdrEncAvatarLen:], uint32(len(encAvatar)))

	pos := sizeHeaderFixed
	copy(out[pos:], encName)
	pos += len(encName)
	copy(out[pos:], encDesc)
	pos += len(encDesc)
	copy(out[pos:], encAvatar)
	return out
}

func buildBody(c *Conversation) []byte {
	var body []byte

	if c.MembersCRDT != nil {
		c.MembersCRDT.mu.RLock()
		// Stable iteration: by tag bytes (raw OR-Set tag string).
		for tag, entry := range c.MembersCRDT.Added {
			body = append(body, encodeMemberEntry([]byte(tag), entry, c.MembersCRDT.Removed[tag])...)
		}
		c.MembersCRDT.mu.RUnlock()
	}

	if c.ReadMarkersCRDT != nil {
		c.ReadMarkersCRDT.mu.RLock()
		for _, marker := range c.ReadMarkersCRDT.Markers {
			body = append(body, encodeReadMarker(marker)...)
		}
		c.ReadMarkersCRDT.mu.RUnlock()
	}

	for _, key := range c.EncryptedKeys {
		body = append(body, encodeEncryptedKey(key)...)
	}

	return body
}

func encodeMemberEntry(tag []byte, m *MemberEntry, removedAt time.Time) []byte {
	role := []byte(m.Role)
	nick := m.EncryptedNickname
	payloadLen := memPayloadFixed + len(role) + len(nick)
	out := make([]byte, 5+payloadLen) // 4-byte length prefix + 1-byte kind + payload
	binary.LittleEndian.PutUint32(out[0:4], uint32(1+payloadLen))
	out[4] = entryKindMember

	p := out[5:]
	if !removedAt.IsZero() {
		p[memOffRemoved] = 1
	}
	binary.LittleEndian.PutUint64(p[memOffAddedAtUnixNs:], uint64(m.AddedAt.UnixNano()))
	if !removedAt.IsZero() {
		binary.LittleEndian.PutUint64(p[memOffRemovedAtUnixNs:], uint64(removedAt.UnixNano()))
	}
	// Tag is up to 16 bytes; pad with zero.
	copyTag := make([]byte, 16)
	copy(copyTag, tag)
	copy(p[memOffTag:], copyTag)
	copy(p[memOffAccountID:], m.AccountID[:])
	copy(p[memOffAddedBy:], m.AddedBy[:])
	binary.LittleEndian.PutUint32(p[memOffRoleLen:], uint32(len(role)))
	binary.LittleEndian.PutUint32(p[memOffNicknameLen:], uint32(len(nick)))
	copy(p[memPayloadFixed:], role)
	copy(p[memPayloadFixed+len(role):], nick)
	return out
}

func encodeReadMarker(m *ReadMarker) []byte {
	out := make([]byte, 5+rmPayloadSize)
	binary.LittleEndian.PutUint32(out[0:4], uint32(1+rmPayloadSize))
	out[4] = entryKindReadMarker
	p := out[5:]
	copy(p[rmOffAccountID:], m.AccountID[:])
	lr := m.LastReadID
	copy(p[rmOffLastReadID:], lr[:])
	binary.LittleEndian.PutUint64(p[rmOffLastReadUnixNs:], uint64(m.LastReadTime.UnixNano()))
	binary.LittleEndian.PutUint64(p[rmOffUpdatedAtUnixNs:], uint64(m.UpdatedAt.UnixNano()))
	return out
}

func encodeEncryptedKey(key []byte) []byte {
	out := make([]byte, 5+len(key))
	binary.LittleEndian.PutUint32(out[0:4], uint32(1+len(key)))
	out[4] = entryKindEncryptedKey
	copy(out[5:], key)
	return out
}

func parseBody(body []byte, c *Conversation) error {
	for len(body) > 0 {
		if len(body) < 5 {
			return fmt.Errorf("%w: body trailing %d bytes", ErrCorruptConversation, len(body))
		}
		entryLen := binary.LittleEndian.Uint32(body[0:4])
		if uint32(len(body)) < 4+entryLen {
			return fmt.Errorf("%w: entry len %d exceeds remaining %d", ErrCorruptConversation, entryLen, len(body)-4)
		}
		kind := body[4]
		payload := body[5 : 4+entryLen]
		body = body[4+entryLen:]

		switch kind {
		case entryKindMember:
			if err := decodeMemberInto(c.MembersCRDT, payload); err != nil {
				return err
			}
		case entryKindReadMarker:
			if err := decodeReadMarkerInto(c.ReadMarkersCRDT, payload); err != nil {
				return err
			}
		case entryKindEncryptedKey:
			c.EncryptedKeys = append(c.EncryptedKeys, append([]byte(nil), payload...))
		default:
			return fmt.Errorf("%w: unknown entry kind 0x%02x", ErrCorruptConversation, kind)
		}
	}
	return nil
}

func decodeMemberInto(m *MembershipCRDT, p []byte) error {
	if len(p) < memPayloadFixed {
		return fmt.Errorf("%w: member payload %d < %d", ErrCorruptConversation, len(p), memPayloadFixed)
	}
	roleLen := binary.LittleEndian.Uint32(p[memOffRoleLen:])
	nickLen := binary.LittleEndian.Uint32(p[memOffNicknameLen:])
	if uint32(len(p)) < uint32(memPayloadFixed)+roleLen+nickLen {
		return fmt.Errorf("%w: member tail %d < %d", ErrCorruptConversation, len(p), uint32(memPayloadFixed)+roleLen+nickLen)
	}

	entry := &MemberEntry{
		AddedAt: time.Unix(0, int64(binary.LittleEndian.Uint64(p[memOffAddedAtUnixNs:]))),
	}
	copy(entry.AccountID[:], p[memOffAccountID:memOffAccountID+32])
	copy(entry.AddedBy[:], p[memOffAddedBy:memOffAddedBy+32])

	tailStart := uint32(memPayloadFixed)
	if roleLen > 0 {
		entry.Role = string(p[tailStart : tailStart+roleLen])
		tailStart += roleLen
	}
	if nickLen > 0 {
		entry.EncryptedNickname = append([]byte(nil), p[tailStart:tailStart+nickLen]...)
	}

	tag := make([]byte, 16)
	copy(tag, p[memOffTag:memOffTag+16])

	m.mu.Lock()
	defer m.mu.Unlock()
	m.Added[string(tag)] = entry
	if p[memOffRemoved] == 1 {
		m.Removed[string(tag)] = time.Unix(0, int64(binary.LittleEndian.Uint64(p[memOffRemovedAtUnixNs:])))
	}
	return nil
}

func decodeReadMarkerInto(r *ReadMarkerCRDT, p []byte) error {
	if len(p) < rmPayloadSize {
		return fmt.Errorf("%w: marker payload %d < %d", ErrCorruptConversation, len(p), rmPayloadSize)
	}
	var accountID [32]byte
	copy(accountID[:], p[rmOffAccountID:rmOffAccountID+32])
	var lastReadID ids.ID
	copy(lastReadID[:], p[rmOffLastReadID:rmOffLastReadID+32])

	marker := &ReadMarker{
		AccountID:    accountID,
		LastReadID:   lastReadID,
		LastReadTime: time.Unix(0, int64(binary.LittleEndian.Uint64(p[rmOffLastReadUnixNs:]))),
		UpdatedAt:    time.Unix(0, int64(binary.LittleEndian.Uint64(p[rmOffUpdatedAtUnixNs:]))),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Markers[string(accountID[:])] = marker
	return nil
}

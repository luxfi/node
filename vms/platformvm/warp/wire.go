// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

// Native-ZAP struct-is-wire for every warp type — no codec, no reflection.
// Each dispatched type carries a 1-byte wkind discriminator at object offset
// 0. The wkind values are the on-wire type ids (formerly the linearcodec
// registration order documented in the retired codec.go):
//
//	0x00 BitSetSignature
//	0x01 CoronaSignature
//	0x02 EncryptedWarpPayload
//	0x03 HybridBLSCoronaSignature
//	0x04 TeleportMessage
//	0x05 TeleportTransferPayload
//	0x06 TeleportAttestPayload
//
// The discriminator is DATA, not registration order: appending a new type
// appends a new id — never reorder, never reuse.

import (
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var ErrUnknownWireKind = errors.New("warp: unknown wire kind")

type wkind uint8

const (
	wkindBitSetSignature          wkind = 0x00
	wkindCoronaSignature          wkind = 0x01
	wkindEncryptedWarpPayload     wkind = 0x02
	wkindHybridBLSCoronaSignature wkind = 0x03
	wkindTeleportMessage          wkind = 0x04
	wkindTeleportTransferPayload  wkind = 0x05
	wkindTeleportAttestPayload    wkind = 0x06

	offWKind = 0
)

func readWireID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, 32))
	return id
}

// ---- BitSetSignature (0x00): kind u8 @0, Signature 96B @1, Signers bytes @97 ----
const (
	bssOffSignature = 1
	bssOffSigners   = 97
	bssSize         = 105
)

func marshalBitSetSignature(s *BitSetSignature) []byte {
	b := zap.NewBuilder(zap.HeaderSize + bssSize + len(s.Signers) + 16)
	ob := b.StartObject(bssSize)
	ob.SetUint8(offWKind, uint8(wkindBitSetSignature))
	ob.SetBytesFixed(bssOffSignature, s.Signature[:])
	ob.SetBytes(bssOffSigners, s.Signers)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseBitSetSignature(root zap.Object) *BitSetSignature {
	s := &BitSetSignature{
		Signers: append([]byte(nil), root.Bytes(bssOffSigners)...),
	}
	copy(s.Signature[:], root.BytesFixedSlice(bssOffSignature, bls.SignatureLen))
	return s
}

// ---- CoronaSignature (0x01): kind u8 @0, Signers bytes @1, Signature bytes @9 ----
const (
	csOffSigners   = 1
	csOffSignature = 9
	csSize         = 17
)

func marshalCoronaSignature(s *CoronaSignature) []byte {
	b := zap.NewBuilder(zap.HeaderSize + csSize + len(s.Signers) + len(s.Signature) + 16)
	ob := b.StartObject(csSize)
	ob.SetUint8(offWKind, uint8(wkindCoronaSignature))
	ob.SetBytes(csOffSigners, s.Signers)
	ob.SetBytes(csOffSignature, s.Signature)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseCoronaSignature(root zap.Object) *CoronaSignature {
	return &CoronaSignature{
		Signers:   append([]byte(nil), root.Bytes(csOffSigners)...),
		Signature: append([]byte(nil), root.Bytes(csOffSignature)...),
	}
}

// ---- EncryptedWarpPayload (0x02): kind u8 @0, EncapsulatedKey @1, Nonce @9,
// Ciphertext @17, RecipientKeyID @25 ----
const (
	ewpOffEncapKey   = 1
	ewpOffNonce      = 9
	ewpOffCiphertext = 17
	ewpOffKeyID      = 25
	ewpSize          = 33
)

func marshalEncryptedWarpPayload(p *EncryptedWarpPayload) []byte {
	b := zap.NewBuilder(zap.HeaderSize + ewpSize +
		len(p.EncapsulatedKey) + len(p.Nonce) + len(p.Ciphertext) + len(p.RecipientKeyID) + 32)
	ob := b.StartObject(ewpSize)
	ob.SetUint8(offWKind, uint8(wkindEncryptedWarpPayload))
	ob.SetBytes(ewpOffEncapKey, p.EncapsulatedKey)
	ob.SetBytes(ewpOffNonce, p.Nonce)
	ob.SetBytes(ewpOffCiphertext, p.Ciphertext)
	ob.SetBytes(ewpOffKeyID, p.RecipientKeyID)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseEncryptedWarpPayload(root zap.Object) *EncryptedWarpPayload {
	return &EncryptedWarpPayload{
		EncapsulatedKey: append([]byte(nil), root.Bytes(ewpOffEncapKey)...),
		Nonce:           append([]byte(nil), root.Bytes(ewpOffNonce)...),
		Ciphertext:      append([]byte(nil), root.Bytes(ewpOffCiphertext)...),
		RecipientKeyID:  append([]byte(nil), root.Bytes(ewpOffKeyID)...),
	}
}

// ---- HybridBLSCoronaSignature (0x03): kind u8 @0, BLSSignature 96B @1,
// Signers @97, CoronaSignature @105, CoronaPKLens u32-list @113,
// CoronaPKBlob @121 ----
const (
	hbcOffBLSSig    = 1
	hbcOffSigners   = 97
	hbcOffCoronaSig = 105
	hbcOffPKLens    = 113
	hbcOffPKBlob    = 121
	hbcSize         = 129
)

func marshalHybridBLSCoronaSignature(s *HybridBLSCoronaSignature) []byte {
	blobLen := 0
	for _, pk := range s.CoronaPublicKeys {
		blobLen += len(pk)
	}
	b := zap.NewBuilder(zap.HeaderSize + hbcSize +
		len(s.Signers) + len(s.CoronaSignature) + blobLen + 4*len(s.CoronaPublicKeys) + 64)

	lensOff, lensCount := 0, 0
	var pkBlob []byte
	if len(s.CoronaPublicKeys) > 0 {
		lb := b.StartList(4)
		for _, pk := range s.CoronaPublicKeys {
			lb.AddUint32(uint32(len(pk)))
			pkBlob = append(pkBlob, pk...)
		}
		lensOff, lensCount = lb.Finish()
	}

	ob := b.StartObject(hbcSize)
	ob.SetUint8(offWKind, uint8(wkindHybridBLSCoronaSignature))
	ob.SetBytesFixed(hbcOffBLSSig, s.BLSSignature[:])
	ob.SetBytes(hbcOffSigners, s.Signers)
	ob.SetBytes(hbcOffCoronaSig, s.CoronaSignature)
	ob.SetList(hbcOffPKLens, lensOff, lensCount)
	ob.SetBytes(hbcOffPKBlob, pkBlob)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseHybridBLSCoronaSignature(root zap.Object) *HybridBLSCoronaSignature {
	s := &HybridBLSCoronaSignature{
		Signers:         append([]byte(nil), root.Bytes(hbcOffSigners)...),
		CoronaSignature: append([]byte(nil), root.Bytes(hbcOffCoronaSig)...),
	}
	copy(s.BLSSignature[:], root.BytesFixedSlice(hbcOffBLSSig, bls.SignatureLen))
	lens := root.ListStride(hbcOffPKLens, 4)
	blob := root.Bytes(hbcOffPKBlob)
	n := lens.Len()
	if n > 0 {
		s.CoronaPublicKeys = make([][]byte, 0, n)
		pos := 0
		for i := 0; i < n; i++ {
			l := int(lens.Uint32(i))
			if pos+l > len(blob) {
				break // malformed lens; keep what parsed cleanly
			}
			s.CoronaPublicKeys = append(s.CoronaPublicKeys, append([]byte(nil), blob[pos:pos+l]...))
			pos += l
		}
	}
	return s
}

// ---- TeleportMessage (0x04): kind u8 @0, Version u8 @1, MessageType u8 @2,
// Encrypted u8 @3, SourceChainID 32B @4, DestChainID 32B @36, Nonce u64 @68,
// Payload bytes @76 ----
const (
	tmOffVersion   = 1
	tmOffType      = 2
	tmOffEncrypted = 3
	tmOffSource    = 4
	tmOffDest      = 36
	tmOffNonce     = 68
	tmOffPayload   = 76
	tmSize         = 84
)

func marshalTeleportMessage(t *TeleportMessage) []byte {
	b := zap.NewBuilder(zap.HeaderSize + tmSize + len(t.Payload) + 16)
	ob := b.StartObject(tmSize)
	ob.SetUint8(offWKind, uint8(wkindTeleportMessage))
	ob.SetUint8(tmOffVersion, t.Version)
	ob.SetUint8(tmOffType, uint8(t.MessageType))
	var enc uint8
	if t.Encrypted {
		enc = 1
	}
	ob.SetUint8(tmOffEncrypted, enc)
	ob.SetBytesFixed(tmOffSource, t.SourceChainID[:])
	ob.SetBytesFixed(tmOffDest, t.DestChainID[:])
	ob.SetUint64(tmOffNonce, t.Nonce)
	ob.SetBytes(tmOffPayload, t.Payload)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseTeleportMessage(root zap.Object) *TeleportMessage {
	return &TeleportMessage{
		Version:       root.Uint8(tmOffVersion),
		MessageType:   TeleportType(root.Uint8(tmOffType)),
		Encrypted:     root.Uint8(tmOffEncrypted) != 0,
		SourceChainID: readWireID(root, tmOffSource),
		DestChainID:   readWireID(root, tmOffDest),
		Nonce:         root.Uint64(tmOffNonce),
		Payload:       append([]byte(nil), root.Bytes(tmOffPayload)...),
	}
}

// ---- TeleportTransferPayload (0x05): kind u8 @0, AssetID 32B @1, Amount u64
// @33, Fee u64 @41, Sender bytes @49, Recipient bytes @57, Memo bytes @65 ----
const (
	ttpOffAssetID   = 1
	ttpOffAmount    = 33
	ttpOffFee       = 41
	ttpOffSender    = 49
	ttpOffRecipient = 57
	ttpOffMemo      = 65
	ttpSize         = 73
)

func marshalTeleportTransferPayload(p *TeleportTransferPayload) []byte {
	b := zap.NewBuilder(zap.HeaderSize + ttpSize +
		len(p.Sender) + len(p.Recipient) + len(p.Memo) + 32)
	ob := b.StartObject(ttpSize)
	ob.SetUint8(offWKind, uint8(wkindTeleportTransferPayload))
	ob.SetBytesFixed(ttpOffAssetID, p.AssetID[:])
	ob.SetUint64(ttpOffAmount, p.Amount)
	ob.SetUint64(ttpOffFee, p.Fee)
	ob.SetBytes(ttpOffSender, p.Sender)
	ob.SetBytes(ttpOffRecipient, p.Recipient)
	ob.SetBytes(ttpOffMemo, p.Memo)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseTeleportTransferPayload(root zap.Object) *TeleportTransferPayload {
	return &TeleportTransferPayload{
		AssetID:   readWireID(root, ttpOffAssetID),
		Amount:    root.Uint64(ttpOffAmount),
		Fee:       root.Uint64(ttpOffFee),
		Sender:    append([]byte(nil), root.Bytes(ttpOffSender)...),
		Recipient: append([]byte(nil), root.Bytes(ttpOffRecipient)...),
		Memo:      append([]byte(nil), root.Bytes(ttpOffMemo)...),
	}
}

// ---- TeleportAttestPayload (0x06): kind u8 @0, AttestationType u8 @1,
// AttesterID 20B @2, Timestamp u64 @22, Data bytes @30 ----
const (
	tapOffType       = 1
	tapOffAttesterID = 2
	tapOffTimestamp  = 22
	tapOffData       = 30
	tapSize          = 38
)

func marshalTeleportAttestPayload(p *TeleportAttestPayload) []byte {
	b := zap.NewBuilder(zap.HeaderSize + tapSize + len(p.Data) + 16)
	ob := b.StartObject(tapSize)
	ob.SetUint8(offWKind, uint8(wkindTeleportAttestPayload))
	ob.SetUint8(tapOffType, p.AttestationType)
	ob.SetBytesFixed(tapOffAttesterID, p.AttesterID[:])
	ob.SetUint64(tapOffTimestamp, p.Timestamp)
	ob.SetBytes(tapOffData, p.Data)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseTeleportAttestPayload(root zap.Object) *TeleportAttestPayload {
	p := &TeleportAttestPayload{
		AttestationType: root.Uint8(tapOffType),
		Timestamp:       root.Uint64(tapOffTimestamp),
		Data:            append([]byte(nil), root.Bytes(tapOffData)...),
	}
	copy(p.AttesterID[:], root.BytesFixedSlice(tapOffAttesterID, 20))
	return p
}

// ---- Signature dispatch ----

// marshalSignature encodes any Signature implementation to its wkind-tagged
// native buffer.
func marshalSignature(s Signature) ([]byte, error) {
	switch sig := s.(type) {
	case *BitSetSignature:
		return marshalBitSetSignature(sig), nil
	case *CoronaSignature:
		return marshalCoronaSignature(sig), nil
	case *HybridBLSCoronaSignature:
		return marshalHybridBLSCoronaSignature(sig), nil
	default:
		return nil, fmt.Errorf("%w: unsupported signature %T", ErrUnknownWireKind, s)
	}
}

// parseSignature dispatches a wkind-tagged buffer to the concrete Signature.
func parseSignature(b []byte) (Signature, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	switch k := wkind(root.Uint8(offWKind)); k {
	case wkindBitSetSignature:
		return parseBitSetSignature(root), nil
	case wkindCoronaSignature:
		return parseCoronaSignature(root), nil
	case wkindHybridBLSCoronaSignature:
		return parseHybridBLSCoronaSignature(root), nil
	default:
		return nil, fmt.Errorf("%w: %d is not a signature kind", ErrUnknownWireKind, k)
	}
}

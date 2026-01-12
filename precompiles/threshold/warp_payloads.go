// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"encoding/binary"
	"errors"
	"time"
)

// Warp payload type identifiers for threshold operations
const (
	PayloadVersionV1 uint8 = 0x01

	// Threshold operation types
	PayloadTypeKeygenRequest  uint8 = 0x10
	PayloadTypeKeygenResult   uint8 = 0x11
	PayloadTypeSignRequest    uint8 = 0x12
	PayloadTypeSignResult     uint8 = 0x13
	PayloadTypeRefreshRequest uint8 = 0x14
	PayloadTypeRefreshResult  uint8 = 0x15
	PayloadTypeReshareRequest uint8 = 0x16
	PayloadTypeReshareResult  uint8 = 0x17
	PayloadTypeQueryRequest   uint8 = 0x18
	PayloadTypeQueryResult    uint8 = 0x19
)

// Result status codes
const (
	ResultStatusSuccess uint8 = 0x00
	ResultStatusFailed  uint8 = 0x01
	ResultStatusPending uint8 = 0x02
	ResultStatusExpired uint8 = 0x03
)

var (
	ErrPayloadTooShort       = errors.New("payload too short")
	ErrInvalidPayloadVersion = errors.New("invalid payload version")
	ErrInvalidPayloadType    = errors.New("invalid payload type")
)

// KeygenRequestPayload is the Warp payload for DKG requests to T-Chain.
// Wire format:
//
//	[0]:      version (1 byte)
//	[1]:      type (1 byte) = 0x10
//	[2:34]:   request_id (32 bytes)
//	[34:66]:  key_id (32 bytes)
//	[66:98]:  source_chain_id (32 bytes)
//	[98]:     protocol (1 byte) - 0=LSS, 1=CMP, 2=BLS, 3=Ringtail, 4=FROST
//	[99]:     threshold (1 byte)
//	[100]:    total_parties (1 byte)
//	[101:109]: nonce (8 bytes)
//	[109:117]: expiry (8 bytes, unix timestamp)
//	[117:137]: requester (20 bytes, caller address)
type KeygenRequestPayload struct {
	RequestID     [32]byte
	KeyID         [32]byte
	SourceChainID [32]byte
	Protocol      uint8
	Threshold     uint8
	TotalParties  uint8
	Nonce         uint64
	Expiry        int64
	Requester     [20]byte
}

// Bytes serializes the keygen request to wire format.
func (p *KeygenRequestPayload) Bytes() []byte {
	buf := make([]byte, 137)
	offset := 0

	buf[offset] = PayloadVersionV1
	offset++
	buf[offset] = PayloadTypeKeygenRequest
	offset++

	copy(buf[offset:], p.RequestID[:])
	offset += 32
	copy(buf[offset:], p.KeyID[:])
	offset += 32
	copy(buf[offset:], p.SourceChainID[:])
	offset += 32

	buf[offset] = p.Protocol
	offset++
	buf[offset] = p.Threshold
	offset++
	buf[offset] = p.TotalParties
	offset++

	binary.BigEndian.PutUint64(buf[offset:], p.Nonce)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(p.Expiry))
	offset += 8

	copy(buf[offset:], p.Requester[:])

	return buf
}

// ParseKeygenRequestPayload parses a keygen request from wire format.
func ParseKeygenRequestPayload(data []byte) (*KeygenRequestPayload, error) {
	if len(data) < 137 {
		return nil, ErrPayloadTooShort
	}

	if data[0] != PayloadVersionV1 {
		return nil, ErrInvalidPayloadVersion
	}
	if data[1] != PayloadTypeKeygenRequest {
		return nil, ErrInvalidPayloadType
	}

	p := &KeygenRequestPayload{}
	offset := 2

	copy(p.RequestID[:], data[offset:])
	offset += 32
	copy(p.KeyID[:], data[offset:])
	offset += 32
	copy(p.SourceChainID[:], data[offset:])
	offset += 32

	p.Protocol = data[offset]
	offset++
	p.Threshold = data[offset]
	offset++
	p.TotalParties = data[offset]
	offset++

	p.Nonce = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	p.Expiry = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	copy(p.Requester[:], data[offset:])

	return p, nil
}

// SignRequestPayload is the Warp payload for threshold signing requests.
// Wire format:
//
//	[0]:      version (1 byte)
//	[1]:      type (1 byte) = 0x12
//	[2:34]:   request_id (32 bytes)
//	[34:66]:  key_id (32 bytes)
//	[66:98]:  message_hash (32 bytes)
//	[98:130]: source_chain_id (32 bytes)
//	[130:138]: nonce (8 bytes)
//	[138:146]: expiry (8 bytes)
//	[146:166]: requester (20 bytes)
//	[166:186]: callback (20 bytes)
//	[186:190]: callback_selector (4 bytes)
type SignRequestPayload struct {
	RequestID        [32]byte
	KeyID            [32]byte
	MessageHash      [32]byte
	SourceChainID    [32]byte
	Nonce            uint64
	Expiry           int64
	Requester        [20]byte
	Callback         [20]byte
	CallbackSelector [4]byte
}

// Bytes serializes the sign request to wire format.
func (p *SignRequestPayload) Bytes() []byte {
	buf := make([]byte, 190)
	offset := 0

	buf[offset] = PayloadVersionV1
	offset++
	buf[offset] = PayloadTypeSignRequest
	offset++

	copy(buf[offset:], p.RequestID[:])
	offset += 32
	copy(buf[offset:], p.KeyID[:])
	offset += 32
	copy(buf[offset:], p.MessageHash[:])
	offset += 32
	copy(buf[offset:], p.SourceChainID[:])
	offset += 32

	binary.BigEndian.PutUint64(buf[offset:], p.Nonce)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(p.Expiry))
	offset += 8

	copy(buf[offset:], p.Requester[:])
	offset += 20
	copy(buf[offset:], p.Callback[:])
	offset += 20
	copy(buf[offset:], p.CallbackSelector[:])

	return buf
}

// ParseSignRequestPayload parses a sign request from wire format.
func ParseSignRequestPayload(data []byte) (*SignRequestPayload, error) {
	if len(data) < 190 {
		return nil, ErrPayloadTooShort
	}

	if data[0] != PayloadVersionV1 {
		return nil, ErrInvalidPayloadVersion
	}
	if data[1] != PayloadTypeSignRequest {
		return nil, ErrInvalidPayloadType
	}

	p := &SignRequestPayload{}
	offset := 2

	copy(p.RequestID[:], data[offset:])
	offset += 32
	copy(p.KeyID[:], data[offset:])
	offset += 32
	copy(p.MessageHash[:], data[offset:])
	offset += 32
	copy(p.SourceChainID[:], data[offset:])
	offset += 32

	p.Nonce = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	p.Expiry = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	copy(p.Requester[:], data[offset:])
	offset += 20
	copy(p.Callback[:], data[offset:])
	offset += 20
	copy(p.CallbackSelector[:], data[offset:])

	return p, nil
}

// SignResultPayload is the Warp payload for signature results from T-Chain.
// Wire format:
//
//	[0]:      version (1 byte)
//	[1]:      type (1 byte) = 0x13
//	[2:34]:   request_id (32 bytes)
//	[34]:     status (1 byte)
//	[35:67]:  r (32 bytes)
//	[67:99]:  s (32 bytes)
//	[99]:     v (1 byte)
//	[100:132]: committee_signature (32 bytes) - aggregated validator signature
type SignResultPayload struct {
	RequestID          [32]byte
	Status             uint8
	R                  [32]byte
	S                  [32]byte
	V                  uint8
	CommitteeSignature [32]byte
}

// Bytes serializes the sign result to wire format.
func (p *SignResultPayload) Bytes() []byte {
	buf := make([]byte, 132)
	offset := 0

	buf[offset] = PayloadVersionV1
	offset++
	buf[offset] = PayloadTypeSignResult
	offset++

	copy(buf[offset:], p.RequestID[:])
	offset += 32

	buf[offset] = p.Status
	offset++

	copy(buf[offset:], p.R[:])
	offset += 32
	copy(buf[offset:], p.S[:])
	offset += 32
	buf[offset] = p.V
	offset++

	copy(buf[offset:], p.CommitteeSignature[:])

	return buf
}

// ParseSignResultPayload parses a sign result from wire format.
func ParseSignResultPayload(data []byte) (*SignResultPayload, error) {
	if len(data) < 132 {
		return nil, ErrPayloadTooShort
	}

	if data[0] != PayloadVersionV1 {
		return nil, ErrInvalidPayloadVersion
	}
	if data[1] != PayloadTypeSignResult {
		return nil, ErrInvalidPayloadType
	}

	p := &SignResultPayload{}
	offset := 2

	copy(p.RequestID[:], data[offset:])
	offset += 32

	p.Status = data[offset]
	offset++

	copy(p.R[:], data[offset:])
	offset += 32
	copy(p.S[:], data[offset:])
	offset += 32
	p.V = data[offset]
	offset++

	copy(p.CommitteeSignature[:], data[offset:])

	return p, nil
}

// RefreshRequestPayload is the Warp payload for key share refresh requests.
type RefreshRequestPayload struct {
	RequestID     [32]byte
	KeyID         [32]byte
	SourceChainID [32]byte
	Nonce         uint64
	Expiry        int64
	Requester     [20]byte
}

// Bytes serializes the refresh request to wire format.
func (p *RefreshRequestPayload) Bytes() []byte {
	buf := make([]byte, 126)
	offset := 0

	buf[offset] = PayloadVersionV1
	offset++
	buf[offset] = PayloadTypeRefreshRequest
	offset++

	copy(buf[offset:], p.RequestID[:])
	offset += 32
	copy(buf[offset:], p.KeyID[:])
	offset += 32
	copy(buf[offset:], p.SourceChainID[:])
	offset += 32

	binary.BigEndian.PutUint64(buf[offset:], p.Nonce)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(p.Expiry))
	offset += 8

	copy(buf[offset:], p.Requester[:])

	return buf
}

// ReshareRequestPayload is the Warp payload for key resharing requests.
type ReshareRequestPayload struct {
	RequestID       [32]byte
	KeyID           [32]byte
	SourceChainID   [32]byte
	NewThreshold    uint8
	NumParticipants uint8
	Participants    [][20]byte // Variable length
	Nonce           uint64
	Expiry          int64
	Requester       [20]byte
}

// Bytes serializes the reshare request to wire format.
func (p *ReshareRequestPayload) Bytes() []byte {
	baseSize := 128 + len(p.Participants)*20
	buf := make([]byte, baseSize)
	offset := 0

	buf[offset] = PayloadVersionV1
	offset++
	buf[offset] = PayloadTypeReshareRequest
	offset++

	copy(buf[offset:], p.RequestID[:])
	offset += 32
	copy(buf[offset:], p.KeyID[:])
	offset += 32
	copy(buf[offset:], p.SourceChainID[:])
	offset += 32

	buf[offset] = p.NewThreshold
	offset++
	buf[offset] = uint8(len(p.Participants))
	offset++

	for _, participant := range p.Participants {
		copy(buf[offset:], participant[:])
		offset += 20
	}

	binary.BigEndian.PutUint64(buf[offset:], p.Nonce)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(p.Expiry))
	offset += 8

	copy(buf[offset:], p.Requester[:])

	return buf
}

// Helper functions for encoding payloads

func encodeKeygenRequest(keyID [32]byte, protocol string, threshold, totalParties uint8) []byte {
	payload := &KeygenRequestPayload{
		KeyID:        keyID,
		Protocol:     protocolToCode(protocol),
		Threshold:    threshold,
		TotalParties: totalParties,
		Nonce:        uint64(time.Now().UnixNano()),
		Expiry:       time.Now().Add(5 * time.Minute).Unix(),
	}
	// Generate request ID from key ID
	copy(payload.RequestID[:], keyID[:])
	return payload.Bytes()
}

func encodeSignRequest(keyID, messageHash [32]byte) []byte {
	payload := &SignRequestPayload{
		KeyID:       keyID,
		MessageHash: messageHash,
		Nonce:       uint64(time.Now().UnixNano()),
		Expiry:      time.Now().Add(5 * time.Minute).Unix(),
	}
	// Generate request ID from key ID + message hash
	for i := 0; i < 32; i++ {
		payload.RequestID[i] = keyID[i] ^ messageHash[i]
	}
	return payload.Bytes()
}

func encodeRefreshRequest(keyID [32]byte) []byte {
	payload := &RefreshRequestPayload{
		KeyID:  keyID,
		Nonce:  uint64(time.Now().UnixNano()),
		Expiry: time.Now().Add(5 * time.Minute).Unix(),
	}
	copy(payload.RequestID[:], keyID[:])
	return payload.Bytes()
}

func encodeReshareRequest(keyID [32]byte, participants [][20]byte, newThreshold uint8) []byte {
	payload := &ReshareRequestPayload{
		KeyID:           keyID,
		NewThreshold:    newThreshold,
		NumParticipants: uint8(len(participants)),
		Participants:    participants,
		Nonce:           uint64(time.Now().UnixNano()),
		Expiry:          time.Now().Add(10 * time.Minute).Unix(),
	}
	copy(payload.RequestID[:], keyID[:])
	return payload.Bytes()
}

func protocolToCode(protocol string) uint8 {
	switch protocol {
	case ProtocolLSS:
		return 0
	case ProtocolCGGMP21:
		return 1
	case ProtocolBLS:
		return 2
	case ProtocolRingtail:
		return 3
	case ProtocolFrost:
		return 4
	default:
		return 0
	}
}

func codeToProtocol(code uint8) string {
	switch code {
	case 0:
		return ProtocolLSS
	case 1:
		return ProtocolCGGMP21
	case 2:
		return ProtocolBLS
	case 3:
		return ProtocolRingtail
	case 4:
		return ProtocolFrost
	default:
		return ProtocolLSS
	}
}

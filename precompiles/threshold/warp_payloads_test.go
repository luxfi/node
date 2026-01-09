// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"bytes"
	"testing"
)

func TestKeygenRequestPayloadRoundTrip(t *testing.T) {
	original := &KeygenRequestPayload{
		RequestID:     [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
		KeyID:         [32]byte{32, 31, 30, 29, 28, 27, 26, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		SourceChainID: [32]byte{0xab, 0xcd},
		Protocol:      1,
		Threshold:     2,
		TotalParties:  5,
		Nonce:         123456789,
		Expiry:        1735689600,
		Requester:     [20]byte{0xde, 0xad, 0xbe, 0xef},
	}

	// Serialize
	data := original.Bytes()

	// Verify length
	if len(data) != 137 {
		t.Fatalf("expected 137 bytes, got %d", len(data))
	}

	// Verify version and type
	if data[0] != PayloadVersionV1 {
		t.Errorf("expected version %d, got %d", PayloadVersionV1, data[0])
	}
	if data[1] != PayloadTypeKeygenRequest {
		t.Errorf("expected type %d, got %d", PayloadTypeKeygenRequest, data[1])
	}

	// Parse
	parsed, err := ParseKeygenRequestPayload(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Compare
	if parsed.RequestID != original.RequestID {
		t.Error("RequestID mismatch")
	}
	if parsed.KeyID != original.KeyID {
		t.Error("KeyID mismatch")
	}
	if parsed.SourceChainID != original.SourceChainID {
		t.Error("SourceChainID mismatch")
	}
	if parsed.Protocol != original.Protocol {
		t.Errorf("Protocol mismatch: got %d, want %d", parsed.Protocol, original.Protocol)
	}
	if parsed.Threshold != original.Threshold {
		t.Errorf("Threshold mismatch: got %d, want %d", parsed.Threshold, original.Threshold)
	}
	if parsed.TotalParties != original.TotalParties {
		t.Errorf("TotalParties mismatch: got %d, want %d", parsed.TotalParties, original.TotalParties)
	}
	if parsed.Nonce != original.Nonce {
		t.Errorf("Nonce mismatch: got %d, want %d", parsed.Nonce, original.Nonce)
	}
	if parsed.Expiry != original.Expiry {
		t.Errorf("Expiry mismatch: got %d, want %d", parsed.Expiry, original.Expiry)
	}
	if parsed.Requester != original.Requester {
		t.Error("Requester mismatch")
	}
}

func TestSignRequestPayloadRoundTrip(t *testing.T) {
	original := &SignRequestPayload{
		RequestID:        [32]byte{1, 2, 3, 4},
		KeyID:            [32]byte{5, 6, 7, 8},
		MessageHash:      [32]byte{9, 10, 11, 12},
		SourceChainID:    [32]byte{13, 14, 15, 16},
		Nonce:            987654321,
		Expiry:           1735689600,
		Requester:        [20]byte{0xaa, 0xbb, 0xcc},
		Callback:         [20]byte{0xdd, 0xee, 0xff},
		CallbackSelector: [4]byte{0x12, 0x34, 0x56, 0x78},
	}

	data := original.Bytes()

	if len(data) != 190 {
		t.Fatalf("expected 190 bytes, got %d", len(data))
	}

	parsed, err := ParseSignRequestPayload(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.RequestID != original.RequestID {
		t.Error("RequestID mismatch")
	}
	if parsed.KeyID != original.KeyID {
		t.Error("KeyID mismatch")
	}
	if parsed.MessageHash != original.MessageHash {
		t.Error("MessageHash mismatch")
	}
	if parsed.Nonce != original.Nonce {
		t.Errorf("Nonce mismatch: got %d, want %d", parsed.Nonce, original.Nonce)
	}
	if parsed.Callback != original.Callback {
		t.Error("Callback mismatch")
	}
	if parsed.CallbackSelector != original.CallbackSelector {
		t.Error("CallbackSelector mismatch")
	}
}

func TestSignResultPayloadRoundTrip(t *testing.T) {
	original := &SignResultPayload{
		RequestID:          [32]byte{1, 2, 3, 4},
		Status:             ResultStatusSuccess,
		R:                  [32]byte{0x11, 0x22, 0x33},
		S:                  [32]byte{0x44, 0x55, 0x66},
		V:                  27,
		CommitteeSignature: [32]byte{0x77, 0x88, 0x99},
	}

	data := original.Bytes()

	if len(data) != 132 {
		t.Fatalf("expected 132 bytes, got %d", len(data))
	}

	parsed, err := ParseSignResultPayload(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.RequestID != original.RequestID {
		t.Error("RequestID mismatch")
	}
	if parsed.Status != original.Status {
		t.Errorf("Status mismatch: got %d, want %d", parsed.Status, original.Status)
	}
	if parsed.R != original.R {
		t.Error("R mismatch")
	}
	if parsed.S != original.S {
		t.Error("S mismatch")
	}
	if parsed.V != original.V {
		t.Errorf("V mismatch: got %d, want %d", parsed.V, original.V)
	}
	if parsed.CommitteeSignature != original.CommitteeSignature {
		t.Error("CommitteeSignature mismatch")
	}
}

func TestRefreshRequestPayloadBytes(t *testing.T) {
	payload := &RefreshRequestPayload{
		RequestID:     [32]byte{1, 2, 3},
		KeyID:         [32]byte{4, 5, 6},
		SourceChainID: [32]byte{7, 8, 9},
		Nonce:         1000,
		Expiry:        2000,
		Requester:     [20]byte{0xaa},
	}

	data := payload.Bytes()

	if len(data) != 126 {
		t.Fatalf("expected 126 bytes, got %d", len(data))
	}

	if data[0] != PayloadVersionV1 {
		t.Errorf("expected version %d, got %d", PayloadVersionV1, data[0])
	}
	if data[1] != PayloadTypeRefreshRequest {
		t.Errorf("expected type %d, got %d", PayloadTypeRefreshRequest, data[1])
	}
}

func TestReshareRequestPayloadBytes(t *testing.T) {
	payload := &ReshareRequestPayload{
		RequestID:       [32]byte{1, 2, 3},
		KeyID:           [32]byte{4, 5, 6},
		SourceChainID:   [32]byte{7, 8, 9},
		NewThreshold:    3,
		NumParticipants: 2,
		Participants: [][20]byte{
			{0xaa, 0xbb, 0xcc},
			{0xdd, 0xee, 0xff},
		},
		Nonce:     3000,
		Expiry:    4000,
		Requester: [20]byte{0x11},
	}

	data := payload.Bytes()

	// Base size (128) + 2 participants * 20 bytes = 168
	expectedLen := 128 + 2*20
	if len(data) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(data))
	}

	if data[0] != PayloadVersionV1 {
		t.Errorf("expected version %d, got %d", PayloadVersionV1, data[0])
	}
	if data[1] != PayloadTypeReshareRequest {
		t.Errorf("expected type %d, got %d", PayloadTypeReshareRequest, data[1])
	}
}

func TestParsePayloadTooShort(t *testing.T) {
	_, err := ParseKeygenRequestPayload([]byte{0x01, 0x10})
	if err != ErrPayloadTooShort {
		t.Errorf("expected ErrPayloadTooShort, got %v", err)
	}

	_, err = ParseSignRequestPayload([]byte{0x01, 0x12})
	if err != ErrPayloadTooShort {
		t.Errorf("expected ErrPayloadTooShort, got %v", err)
	}

	_, err = ParseSignResultPayload([]byte{0x01, 0x13})
	if err != ErrPayloadTooShort {
		t.Errorf("expected ErrPayloadTooShort, got %v", err)
	}
}

func TestParsePayloadInvalidVersion(t *testing.T) {
	// Create valid-length payload with wrong version
	data := make([]byte, 200)
	data[0] = 0xFF // Invalid version
	data[1] = PayloadTypeKeygenRequest

	_, err := ParseKeygenRequestPayload(data)
	if err != ErrInvalidPayloadVersion {
		t.Errorf("expected ErrInvalidPayloadVersion, got %v", err)
	}
}

func TestParsePayloadInvalidType(t *testing.T) {
	// Create valid-length payload with wrong type
	data := make([]byte, 200)
	data[0] = PayloadVersionV1
	data[1] = 0xFF // Invalid type

	_, err := ParseKeygenRequestPayload(data)
	if err != ErrInvalidPayloadType {
		t.Errorf("expected ErrInvalidPayloadType, got %v", err)
	}
}

func TestEncodeKeygenRequest(t *testing.T) {
	keyID := [32]byte{1, 2, 3, 4}
	protocol := ProtocolLSS
	threshold := uint8(2)
	totalParties := uint8(5)

	data := encodeKeygenRequest(keyID, protocol, threshold, totalParties)

	if len(data) != 137 {
		t.Fatalf("expected 137 bytes, got %d", len(data))
	}

	// Verify we can parse it back
	parsed, err := ParseKeygenRequestPayload(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.KeyID != keyID {
		t.Error("KeyID mismatch")
	}
	if parsed.Protocol != 0 { // LSS = 0
		t.Errorf("Protocol mismatch: got %d, want 0", parsed.Protocol)
	}
	if parsed.Threshold != threshold {
		t.Errorf("Threshold mismatch: got %d, want %d", parsed.Threshold, threshold)
	}
	if parsed.TotalParties != totalParties {
		t.Errorf("TotalParties mismatch: got %d, want %d", parsed.TotalParties, totalParties)
	}
}

func TestEncodeSignRequest(t *testing.T) {
	keyID := [32]byte{1, 2, 3, 4}
	messageHash := [32]byte{5, 6, 7, 8}

	data := encodeSignRequest(keyID, messageHash)

	if len(data) != 190 {
		t.Fatalf("expected 190 bytes, got %d", len(data))
	}

	parsed, err := ParseSignRequestPayload(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.KeyID != keyID {
		t.Error("KeyID mismatch")
	}
	if parsed.MessageHash != messageHash {
		t.Error("MessageHash mismatch")
	}

	// Request ID should be XOR of keyID and messageHash
	expectedRequestID := [32]byte{}
	for i := 0; i < 32; i++ {
		expectedRequestID[i] = keyID[i] ^ messageHash[i]
	}
	if parsed.RequestID != expectedRequestID {
		t.Error("RequestID not properly computed")
	}
}

func TestEncodeRefreshRequest(t *testing.T) {
	keyID := [32]byte{1, 2, 3, 4}

	data := encodeRefreshRequest(keyID)

	if len(data) != 126 {
		t.Fatalf("expected 126 bytes, got %d", len(data))
	}

	// Verify version and type
	if data[0] != PayloadVersionV1 {
		t.Errorf("expected version %d, got %d", PayloadVersionV1, data[0])
	}
	if data[1] != PayloadTypeRefreshRequest {
		t.Errorf("expected type %d, got %d", PayloadTypeRefreshRequest, data[1])
	}
}

func TestEncodeReshareRequest(t *testing.T) {
	keyID := [32]byte{1, 2, 3, 4}
	participants := [][20]byte{
		{0xaa, 0xbb},
		{0xcc, 0xdd},
		{0xee, 0xff},
	}
	newThreshold := uint8(2)

	data := encodeReshareRequest(keyID, participants, newThreshold)

	// Base (128) + 3 participants * 20 = 188
	expectedLen := 128 + 3*20
	if len(data) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(data))
	}

	if data[0] != PayloadVersionV1 {
		t.Errorf("expected version %d, got %d", PayloadVersionV1, data[0])
	}
	if data[1] != PayloadTypeReshareRequest {
		t.Errorf("expected type %d, got %d", PayloadTypeReshareRequest, data[1])
	}
}

func TestProtocolCodes(t *testing.T) {
	tests := []struct {
		protocol string
		code     uint8
	}{
		{ProtocolLSS, 0},
		{ProtocolCGGMP21, 1},
		{ProtocolBLS, 2},
		{ProtocolRingtail, 3},
		{ProtocolFrost, 4},
	}

	for _, tc := range tests {
		code := protocolToCode(tc.protocol)
		if code != tc.code {
			t.Errorf("protocolToCode(%s) = %d, want %d", tc.protocol, code, tc.code)
		}

		protocol := codeToProtocol(tc.code)
		if protocol != tc.protocol {
			t.Errorf("codeToProtocol(%d) = %s, want %s", tc.code, protocol, tc.protocol)
		}
	}
}

func TestPayloadBytesImmutability(t *testing.T) {
	original := &KeygenRequestPayload{
		RequestID: [32]byte{1, 2, 3, 4},
		KeyID:     [32]byte{5, 6, 7, 8},
		Threshold: 2,
	}

	data := original.Bytes()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// Modify original
	original.Threshold = 99

	// Verify serialized data wasn't affected
	if !bytes.Equal(data, dataCopy) {
		t.Error("serialized data was affected by modifying original")
	}
}

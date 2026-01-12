// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/luxfi/ids"
)

// MockWarpSender is a mock implementation of WarpSender for testing.
type MockWarpSender struct {
	lastDestChain ids.ID
	lastPayload   []byte
	sendError     error
}

func (m *MockWarpSender) SendCrossChainMessage(destChainID ids.ID, payload []byte) error {
	m.lastDestChain = destChainID
	m.lastPayload = payload
	return m.sendError
}

func TestRequiredGas(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	tests := []struct {
		name     string
		selector [4]byte
		expected uint64
	}{
		{"CreateKey", SelectorCreateKey, GasCreateKey},
		{"Sign", SelectorSign, GasThresholdSign},
		{"Verify", SelectorVerify, GasVerifySignature},
		{"Refresh", SelectorRefresh, GasRefreshShares},
		{"Reshare", SelectorReshare, GasReshareKey},
		{"GetPublicKey", SelectorGetPublicKey, GasGetPublicKey},
		{"GetParticipants", SelectorGetParticipants, GasGetParticipants},
		{"GetSignature", SelectorGetSignature, GasGetPublicKey},
		{"GetStatus", SelectorGetStatus, GasGetPublicKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]byte, 100)
			copy(input[:4], tc.selector[:])
			gas := precompile.RequiredGas(input)
			if gas != tc.expected {
				t.Errorf("expected gas %d, got %d", tc.expected, gas)
			}
		})
	}
}

func TestRequiredGasInvalidInput(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	// Input too short
	gas := precompile.RequiredGas([]byte{0x01, 0x02})
	if gas != 0 {
		t.Errorf("expected gas 0 for short input, got %d", gas)
	}

	// Invalid selector
	gas = precompile.RequiredGas([]byte{0xff, 0xff, 0xff, 0xff})
	if gas != 0 {
		t.Errorf("expected gas 0 for invalid selector, got %d", gas)
	}
}

func TestCreateThresholdKey(t *testing.T) {
	sender := &MockWarpSender{}
	tChainID := ids.GenerateTestID()
	precompile := NewThresholdPrecompile(tChainID, sender)

	// Construct valid input
	// keyId (32 bytes) + protocol (32 bytes) + threshold (1 byte) + totalParties (1 byte)
	input := make([]byte, 4+66)
	copy(input[:4], SelectorCreateKey[:])

	// Key ID
	keyID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], keyID[:])

	// Protocol "lss" (padded to 32 bytes)
	copy(input[36:68], []byte("lss"))

	// Threshold = 2, TotalParties = 3
	input[68] = 2
	input[69] = 3

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 32 {
		t.Errorf("expected 32 byte session ID, got %d bytes", len(result))
	}

	// Verify Warp message was sent to T-Chain
	if sender.lastDestChain != tChainID {
		t.Errorf("expected dest chain %s, got %s", tChainID, sender.lastDestChain)
	}

	if len(sender.lastPayload) == 0 {
		t.Error("expected Warp payload to be sent")
	}

	// Verify payload version and type
	if sender.lastPayload[0] != PayloadVersionV1 {
		t.Errorf("expected payload version %d, got %d", PayloadVersionV1, sender.lastPayload[0])
	}
	if sender.lastPayload[1] != PayloadTypeKeygenRequest {
		t.Errorf("expected payload type %d, got %d", PayloadTypeKeygenRequest, sender.lastPayload[1])
	}
}

func TestCreateThresholdKeyInvalidProtocol(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+66)
	copy(input[:4], SelectorCreateKey[:])

	// Invalid protocol
	copy(input[36:68], []byte("invalid"))
	input[68] = 2
	input[69] = 3

	_, err := precompile.Run(input)
	if err != ErrInvalidProtocol {
		t.Errorf("expected ErrInvalidProtocol, got %v", err)
	}
}

func TestCreateThresholdKeyInvalidThreshold(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+66)
	copy(input[:4], SelectorCreateKey[:])
	copy(input[36:68], []byte("lss"))

	// Invalid: threshold >= totalParties
	input[68] = 3
	input[69] = 3

	_, err := precompile.Run(input)
	if err != ErrInvalidThreshold {
		t.Errorf("expected ErrInvalidThreshold, got %v", err)
	}

	// Invalid: threshold = 0
	input[68] = 0
	input[69] = 3

	_, err = precompile.Run(input)
	if err != ErrInvalidThreshold {
		t.Errorf("expected ErrInvalidThreshold, got %v", err)
	}
}

func TestThresholdSign(t *testing.T) {
	sender := &MockWarpSender{}
	tChainID := ids.GenerateTestID()
	precompile := NewThresholdPrecompile(tChainID, sender)

	input := make([]byte, 4+64)
	copy(input[:4], SelectorSign[:])

	// Key ID
	keyID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], keyID[:])

	// Message hash
	msgHash := [32]byte{5, 6, 7, 8}
	copy(input[36:68], msgHash[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 32 {
		t.Errorf("expected 32 byte session ID, got %d bytes", len(result))
	}

	// Verify payload type
	if sender.lastPayload[1] != PayloadTypeSignRequest {
		t.Errorf("expected payload type %d, got %d", PayloadTypeSignRequest, sender.lastPayload[1])
	}
}

func TestRefreshShares(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+32)
	copy(input[:4], SelectorRefresh[:])

	keyID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], keyID[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 32 {
		t.Errorf("expected 32 byte session ID, got %d bytes", len(result))
	}

	if sender.lastPayload[1] != PayloadTypeRefreshRequest {
		t.Errorf("expected payload type %d, got %d", PayloadTypeRefreshRequest, sender.lastPayload[1])
	}
}

func TestGetPublicKey(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+32)
	copy(input[:4], SelectorGetPublicKey[:])

	keyID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], keyID[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be ABI-encoded bytes
	if len(result) < 64 {
		t.Errorf("expected at least 64 bytes for ABI-encoded response, got %d", len(result))
	}
}

func TestGetParticipants(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+32)
	copy(input[:4], SelectorGetParticipants[:])

	keyID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], keyID[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be ABI-encoded address array
	if len(result) < 64 {
		t.Errorf("expected at least 64 bytes for ABI-encoded response, got %d", len(result))
	}
}

func TestGetSignature(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+32)
	copy(input[:4], SelectorGetSignature[:])

	sessionID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], sessionID[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be r (32) + s (32) + v (32)
	if len(result) != 96 {
		t.Errorf("expected 96 bytes (r+s+v), got %d", len(result))
	}
}

func TestGetSessionStatus(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := make([]byte, 4+32)
	copy(input[:4], SelectorGetStatus[:])

	sessionID := [32]byte{1, 2, 3, 4}
	copy(input[4:36], sessionID[:])

	result, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be status (32) + error string offset (32)
	if len(result) != 64 {
		t.Errorf("expected 64 bytes, got %d", len(result))
	}
}

func TestInvalidSelector(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	input := []byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x00}

	_, err := precompile.Run(input)
	if err != ErrInvalidSelector {
		t.Errorf("expected ErrInvalidSelector, got %v", err)
	}
}

func TestInputTooShort(t *testing.T) {
	sender := &MockWarpSender{}
	precompile := NewThresholdPrecompile(ids.Empty, sender)

	// Input less than 4 bytes
	_, err := precompile.Run([]byte{0x01, 0x02})
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestIsValidProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		valid    bool
	}{
		{ProtocolLSS, true},
		{ProtocolCGGMP21, true},
		{ProtocolBLS, true},
		{ProtocolRingtail, true},
		{ProtocolFrost, true},
		{"invalid", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.protocol, func(t *testing.T) {
			result := isValidProtocol(tc.protocol)
			if result != tc.valid {
				t.Errorf("isValidProtocol(%q) = %v, want %v", tc.protocol, result, tc.valid)
			}
		})
	}
}

func TestProtocolCodeConversion(t *testing.T) {
	protocols := []string{ProtocolLSS, ProtocolCGGMP21, ProtocolBLS, ProtocolRingtail, ProtocolFrost}

	for _, p := range protocols {
		code := protocolToCode(p)
		back := codeToProtocol(code)
		if back != p {
			t.Errorf("protocol round-trip failed: %s -> %d -> %s", p, code, back)
		}
	}
}

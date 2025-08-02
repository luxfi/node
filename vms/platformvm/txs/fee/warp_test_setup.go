// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

// mockWarpMessage implements WarpMessage for testing
type mockWarpMessage struct {
	signature mockWarpSignature
}

func (m *mockWarpMessage) GetSignature() WarpSignature {
	return &m.signature
}

// mockWarpSignature implements WarpSignature for testing
type mockWarpSignature struct {
	numSigners int
}

func (m *mockWarpSignature) NumSigners() (int, error) {
	return m.numSigners, nil
}

// InitializeTestWarpParser sets up a mock warp parser for tests
func InitializeTestWarpParser() {
	SetWarpMessageParser(func(bytes []byte) (WarpMessage, error) {
		// For test purposes, assume a fixed number of signers
		// This matches the test expectations
		return &mockWarpMessage{
			signature: mockWarpSignature{
				numSigners: 1, // Default to 1 signer for tests
			},
		}, nil
	})
}
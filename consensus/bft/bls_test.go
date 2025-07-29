// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bft

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/crypto/bls"
)

func TestBLSVerifier(t *testing.T) {
	config := newEngineConfig(t, 1)
	signer, verifier := NewBLSAuth(config)
	otherNodeID := ids.GenerateTestNodeID()

	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	tests := []struct {
		name      string
		expectErr error
		nodeID    []byte
		sig       []byte
	}{
		{
			name:      "valid_signature",
			expectErr: nil,
			nodeID:    config.Ctx.NodeID[:],
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "not_in_membership_set",
			expectErr: errSignerNotFound,
			nodeID:    otherNodeID[:],
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "invalid_message_encoding",
			expectErr: errSignatureVerificationFailed,
			nodeID:    config.Ctx.NodeID[:],
			sig: func() []byte {
				sig, err := config.SignBLS(msg)
				require.NoError(t, err)
				return bls.SignatureToBytes(sig)
			}(),
		},
		{
			name:      "invalid_nodeID",
			expectErr: errInvalidNodeID,
			nodeID:    []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // Incorrect length NodeID
			sig: func() []byte {
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				return sig
			}(),
		},
		{
			name:      "nil_signature",
			expectErr: errFailedToParseSignature,
			nodeID:    config.Ctx.NodeID[:],
			sig:       nil,
		},
		{
			name:      "malformed_signature",
			expectErr: errFailedToParseSignature,
			nodeID:    config.Ctx.NodeID[:],
			sig:       []byte{0x01, 0x02, 0x03}, // Malformed signature
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.Verify(msg, tt.sig, tt.nodeID)
			require.ErrorIs(t, err, tt.expectErr)
		})
	}
}

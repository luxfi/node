// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

func TestMintOperationVerifyNil(t *testing.T) {
	op := (*MintOperation)(nil)
	err := op.Verify()
	require.ErrorIs(t, err, errNilMintOperation)
}

func TestMintOperationVerifyTooLargePayload(t *testing.T) {
	op := MintOperation{
		Payload: make([]byte, MaxPayloadSize+1),
	}
	err := op.Verify()
	require.ErrorIs(t, err, errPayloadTooLarge)
}

func TestMintOperationVerifyInvalidOutput(t *testing.T) {
	op := MintOperation{
		Outputs: []*secp256k1fx.OutputOwners{{
			Threshold: 1,
		}},
	}
	err := op.Verify()
	require.ErrorIs(t, err, secp256k1fx.ErrOutputUnspendable)
}

func TestMintOperationOuts(t *testing.T) {
	op := MintOperation{
		Outputs: []*secp256k1fx.OutputOwners{{}},
	}
	require.Len(t, op.Outs(), 1)
}

func TestMintOperationState(t *testing.T) {
	intf := interface{}(&MintOperation{})
	_, ok := intf.(verify.State)
	require.False(t, ok)
}

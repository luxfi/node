// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestUnsignedCreateChainTxVerify pins the CreateChainTx syntactic invariants.
// Struct-is-wire has no post-hoc field mutation, so every invalid case is built
// THROUGH the NewCreateChainTx constructor with the offending value baked in.
func TestUnsignedCreateChainTxVerify(t *testing.T) {
	rt := consensustest.Runtime(t, ids.GenerateTestID())
	validChain := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{0, 1}}
	base := func() *lux.BaseTx {
		return &lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}
	}

	tests := []struct {
		description string
		build       func(t *testing.T) *CreateChainTx
		expectedErr error
	}{
		{
			description: "tx is nil",
			build:       func(*testing.T) *CreateChainTx { return nil },
			expectedErr: ErrNilTx,
		},
		{
			description: "vm ID is empty",
			build: func(t *testing.T) *CreateChainTx {
				tx, err := NewCreateChainTx(base(), validChain, "yeet", ids.Empty, nil, nil, auth)
				require.NoError(t, err)
				return tx
			},
			expectedErr: errInvalidVMID,
		},
		{
			description: "chain ID is primary network ID",
			build: func(t *testing.T) *CreateChainTx {
				tx, err := NewCreateChainTx(base(), constants.PrimaryNetworkID, "yeet", constants.XVMID, nil, nil, auth)
				require.NoError(t, err)
				return tx
			},
			expectedErr: ErrCantValidatePrimaryNetwork,
		},
		{
			description: "chain name is too long",
			build: func(t *testing.T) *CreateChainTx {
				tx, err := NewCreateChainTx(base(), validChain, string(make([]byte, MaxNameLen+1)), constants.XVMID, nil, nil, auth)
				require.NoError(t, err)
				return tx
			},
			expectedErr: errNameTooLong,
		},
		{
			description: "chain name has invalid character",
			build: func(t *testing.T) *CreateChainTx {
				tx, err := NewCreateChainTx(base(), validChain, "⌘", constants.XVMID, nil, nil, auth)
				require.NoError(t, err)
				return tx
			},
			expectedErr: errIllegalNameCharacter,
		},
		{
			description: "genesis data is too long",
			build: func(t *testing.T) *CreateChainTx {
				tx, err := NewCreateChainTx(base(), validChain, "yeet", constants.XVMID, nil, make([]byte, MaxGenesisLen+1), auth)
				require.NoError(t, err)
				return tx
			},
			expectedErr: errGenesisTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require.ErrorIs(t, test.build(t).SyntacticVerify(rt), test.expectedErr)
		})
	}
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestDisableL1ValidatorTxRoundTrip builds the tx through its constructor,
// round-trips it through the struct-is-wire path, and confirms the delta fields
// (ValidationID, DisableAuth) and the spending envelope survive encode→decode.
func TestDisableL1ValidatorTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	validationID := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{9}}

	in, err := NewDisableL1ValidatorTx(base, validationID, auth)
	require.NoError(err)

	got := roundTrip(t, in).(*DisableL1ValidatorTx)
	require.Equal(validationID, got.ValidationID())
	require.Equal(auth, got.DisableAuth())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestDisableL1ValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	validationID := ids.GenerateTestID()
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *DisableL1ValidatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: invalid BaseTx (wrong network ID)
	wrongNet, err := NewDisableL1ValidatorTx(base(rt.NetworkID+1), validationID, &secp256k1fx.Input{})
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: invalid disable auth (sig indices not sorted and unique)
	badAuth, err := NewDisableL1ValidatorTx(base(rt.NetworkID), validationID, &secp256k1fx.Input{SigIndices: []uint32{1, 0}})
	require.NoError(err)
	require.ErrorIs(badAuth.SyntacticVerify(rt), secp256k1fx.ErrInputIndicesNotSortedUnique)

	// Case: passes verification
	valid, err := NewDisableL1ValidatorTx(base(rt.NetworkID), validationID, &secp256k1fx.Input{})
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

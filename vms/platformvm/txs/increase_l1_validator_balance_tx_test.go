// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
)

// TestIncreaseL1ValidatorBalanceTxRoundTrip builds the tx through its
// constructor, round-trips it, and confirms the delta fields (ValidationID,
// Balance) and the spending envelope survive encode→decode.
func TestIncreaseL1ValidatorBalanceTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	validationID := ids.GenerateTestID()
	const balance uint64 = 0xfedcba9876543210

	in, err := NewIncreaseL1ValidatorBalanceTx(base, validationID, balance)
	require.NoError(err)

	got := roundTrip(t, in).(*IncreaseL1ValidatorBalanceTx)
	require.Equal(validationID, got.ValidationID())
	require.Equal(balance, got.Balance())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestIncreaseL1ValidatorBalanceTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	validationID := ids.GenerateTestID()
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *IncreaseL1ValidatorBalanceTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: zero balance
	zeroBal, err := NewIncreaseL1ValidatorBalanceTx(base(rt.NetworkID), validationID, 0)
	require.NoError(err)
	require.ErrorIs(zeroBal.SyntacticVerify(rt), ErrZeroBalance)

	// Case: invalid BaseTx (wrong network ID)
	wrongNet, err := NewIncreaseL1ValidatorBalanceTx(base(rt.NetworkID+1), validationID, 1)
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: passes verification
	valid, err := NewIncreaseL1ValidatorBalanceTx(base(rt.NetworkID), validationID, 1)
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

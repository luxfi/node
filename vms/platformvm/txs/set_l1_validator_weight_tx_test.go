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

// TestSetL1ValidatorWeightTxRoundTrip builds the tx through its constructor,
// round-trips it, and confirms the delta field (Message) and the spending
// envelope survive encode→decode.
func TestSetL1ValidatorWeightTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	message := []byte("message")

	in, err := NewSetL1ValidatorWeightTx(base, message)
	require.NoError(err)

	got := roundTrip(t, in).(*SetL1ValidatorWeightTx)
	require.Equal(message, got.Message())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestSetL1ValidatorWeightTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	message := []byte("message")
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *SetL1ValidatorWeightTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: invalid BaseTx (wrong network ID)
	wrongNet, err := NewSetL1ValidatorWeightTx(base(rt.NetworkID+1), message)
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: passes verification
	valid, err := NewSetL1ValidatorWeightTx(base(rt.NetworkID), message)
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

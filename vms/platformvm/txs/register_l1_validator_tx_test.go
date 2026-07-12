// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
)

// TestRegisterL1ValidatorTxRoundTrip builds the tx through its constructor,
// round-trips it, and confirms the delta fields (Balance, ProofOfPossession,
// Message) and the spending envelope survive encode→decode.
func TestRegisterL1ValidatorTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	const balance uint64 = 1_000_000
	var pop [bls.SignatureLen]byte
	for i := range pop {
		pop[i] = byte(i)
	}
	message := []byte("message")

	in, err := NewRegisterL1ValidatorTx(base, balance, pop, message)
	require.NoError(err)

	got := roundTrip(t, in).(*RegisterL1ValidatorTx)
	require.Equal(balance, got.Balance())
	require.Equal(pop, got.ProofOfPossession())
	require.Equal(message, got.Message())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestRegisterL1ValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	var pop [bls.SignatureLen]byte
	message := []byte("message")
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *RegisterL1ValidatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: invalid BaseTx (wrong network ID)
	wrongNet, err := NewRegisterL1ValidatorTx(base(rt.NetworkID+1), 1, pop, message)
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: passes verification
	valid, err := NewRegisterL1ValidatorTx(base(rt.NetworkID), 1, pop, message)
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

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

// TestRemoveChainValidatorTxRoundTrip builds the tx through its constructor,
// round-trips it, and confirms the delta fields (NodeID, Chain, ChainAuth) and
// the spending envelope survive encode→decode.
func TestRemoveChainValidatorTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	nodeID := ids.GenerateTestNodeID()
	chain := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{3}}

	in, err := NewRemoveChainValidatorTx(base, nodeID, chain, auth)
	require.NoError(err)

	got := roundTrip(t, in).(*RemoveChainValidatorTx)
	require.Equal(nodeID, got.NodeID())
	require.Equal(chain, got.Chain())
	require.Equal(auth, got.ChainAuth())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestRemoveChainValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	nodeID := ids.GenerateTestNodeID()
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *RemoveChainValidatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: invalid BaseTx (wrong network ID); Chain and NodeID are set so we
	// don't error on those checks first.
	wrongNet, err := NewRemoveChainValidatorTx(base(rt.NetworkID+1), nodeID, ids.GenerateTestID(), &secp256k1fx.Input{})
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: can't remove a primary network validator
	primary, err := NewRemoveChainValidatorTx(base(rt.NetworkID), nodeID, constants.PrimaryNetworkID, &secp256k1fx.Input{})
	require.NoError(err)
	require.ErrorIs(primary.SyntacticVerify(rt), ErrRemovePrimaryNetworkValidator)

	// Case: invalid chain auth (sig indices not sorted and unique)
	badAuth, err := NewRemoveChainValidatorTx(base(rt.NetworkID), nodeID, ids.GenerateTestID(), &secp256k1fx.Input{SigIndices: []uint32{1, 0}})
	require.NoError(err)
	require.ErrorIs(badAuth.SyntacticVerify(rt), secp256k1fx.ErrInputIndicesNotSortedUnique)

	// Case: passes verification
	valid, err := NewRemoveChainValidatorTx(base(rt.NetworkID), nodeID, ids.GenerateTestID(), &secp256k1fx.Input{})
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

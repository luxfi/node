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

// TestTransferChainOwnershipTxRoundTrip builds the tx through its constructor,
// round-trips it, and confirms the delta fields (Chain, ChainAuth, Owner) and
// the spending envelope survive encode→decode.
func TestTransferChainOwnershipTxRoundTrip(t *testing.T) {
	require := require.New(t)

	base := spendBase()
	chain := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{3}}
	owner := &secp256k1fx.OutputOwners{Locktime: 876543210, Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}

	in, err := NewTransferChainOwnershipTx(base, chain, auth, owner)
	require.NoError(err)

	got := roundTrip(t, in).(*TransferChainOwnershipTx)
	require.Equal(chain, got.Chain())
	require.Equal(auth, got.ChainAuth())
	require.Equal(owner, got.Owner())
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal([]byte(base.Memo), got.Memo())
}

func TestTransferChainOwnershipTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	owner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: nil tx
	var nilTx *TransferChainOwnershipTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: invalid BaseTx (wrong network ID); Chain is set so we don't error on
	// that check first.
	wrongNet, err := NewTransferChainOwnershipTx(base(rt.NetworkID+1), ids.GenerateTestID(), &secp256k1fx.Input{}, owner)
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: cannot transfer ownership of a permissionless chain
	primary, err := NewTransferChainOwnershipTx(base(rt.NetworkID), constants.PrimaryNetworkID, &secp256k1fx.Input{}, owner)
	require.NoError(err)
	require.ErrorIs(primary.SyntacticVerify(rt), ErrTransferPermissionlessChain)

	// Case: invalid chain auth (sig indices not sorted and unique)
	badAuth, err := NewTransferChainOwnershipTx(base(rt.NetworkID), ids.GenerateTestID(), &secp256k1fx.Input{SigIndices: []uint32{1, 0}}, owner)
	require.NoError(err)
	require.ErrorIs(badAuth.SyntacticVerify(rt), secp256k1fx.ErrInputIndicesNotSortedUnique)

	// Case: passes verification
	valid, err := NewTransferChainOwnershipTx(base(rt.NetworkID), ids.GenerateTestID(), &secp256k1fx.Input{}, owner)
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))
}

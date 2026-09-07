// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/utxo/secp256k1fx"

	consensustest "github.com/luxfi/consensus/test/helpers"
	lux "github.com/luxfi/utxo"
)

// TestAddPermissionlessDelegatorTx_RoundTrip exercises the struct-is-wire path
// for both a chain (non-primary) and a primary-network delegator: the delta
// fields round-trip through Parse. This replaces the deleted linearcodec
// golden-byte + JSON serialization tests.
func TestAddPermissionlessDelegatorTx_RoundTrip(t *testing.T) {
	require := require.New(t)

	assetID := ids.GenerateTestID()
	stakeOuts := []*lux.TransferableOutput{
		{Asset: lux.Asset{ID: assetID}, Out: &secp256k1fx.TransferOutput{
			Amt:          3,
			OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		}},
	}
	rewardsOwner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	vdr := Validator{NodeID: ids.GenerateTestNodeID(), Start: 1, End: 100, Wght: 3}

	// Chain (non-primary) delegator.
	netTx, err := NewAddPermissionlessDelegatorTx(spendBase(), vdr, ids.GenerateTestID(), stakeOuts, rewardsOwner)
	require.NoError(err)
	gotNet := roundTrip(t, netTx).(*AddPermissionlessDelegatorTx)
	require.Equal(vdr, gotNet.Validator())
	require.Equal(netTx.Chain(), gotNet.Chain())
	require.Equal(stakeOuts, gotNet.StakeOuts())
	require.Equal(rewardsOwner, gotNet.DelegationRewardsOwner())

	// Primary-network delegator: same delta surface, primary chain id.
	primTx, err := NewAddPermissionlessDelegatorTx(spendBase(), vdr, constants.PrimaryNetworkID, stakeOuts, rewardsOwner)
	require.NoError(err)
	gotPrim := roundTrip(t, primTx).(*AddPermissionlessDelegatorTx)
	require.Equal(constants.PrimaryNetworkID, gotPrim.Chain())
	require.Equal(vdr, gotPrim.Validator())
	require.Equal(stakeOuts, gotPrim.StakeOuts())
	require.Equal(rewardsOwner, gotPrim.DelegationRewardsOwner())
}

func TestAddPermissionlessDelegatorTxSyntacticVerify(t *testing.T) {
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	// Local builders — struct-is-wire has no post-hoc field mutation, so every
	// (in)valid case is expressed by passing values THROUGH the constructor.
	validBase := func() *lux.BaseTx {
		return &lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}
	}
	invalidBase := func() *lux.BaseTx {
		return &lux.BaseTx{NetworkID: 0, BlockchainID: rt.ChainID} // wrong networkID
	}
	goodOwner := func() *secp256k1fx.OutputOwners {
		return &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	}
	out := func(assetID ids.ID, amt uint64) *lux.TransferableOutput {
		return &lux.TransferableOutput{Asset: lux.Asset{ID: assetID}, Out: &secp256k1fx.TransferOutput{Amt: amt}}
	}

	tests := []struct {
		name  string
		build func(t *testing.T) *AddPermissionlessDelegatorTx
		err   error
	}{
		{
			name:  "nil tx",
			build: func(*testing.T) *AddPermissionlessDelegatorTx { return nil },
			err:   ErrNilTx,
		},
		{
			name: "no provided stake",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID()}, ids.GenerateTestID(), nil, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: errNoStake,
		},
		{
			name: "invalid BaseTx",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				tx, err := NewAddPermissionlessDelegatorTx(invalidBase(), Validator{NodeID: ids.GenerateTestNodeID()}, ids.GenerateTestID(), []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: lux.ErrWrongNetworkID,
		},
		{
			// Was: fxmock owner returns errCustom. Reproduced with a real
			// unspendable owner (Threshold > #Addrs) → ErrOutputUnspendable.
			name: "invalid rewards owner",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				badOwner := &secp256k1fx.OutputOwners{Threshold: 1}
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, badOwner)
				require.NoError(t, err)
				return tx
			},
			err: secp256k1fx.ErrOutputUnspendable,
		},
		{
			// Was: luxmock stake-out Verify errCustom. Reproduced with a real
			// unspendable stake output → ErrOutputUnspendable.
			name: "invalid stake output",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				badOut := &lux.TransferableOutput{Asset: lux.Asset{ID: ids.GenerateTestID()}, Out: &secp256k1fx.TransferOutput{Amt: 1, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1}}}
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{badOut}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "multiple staked assets",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{out(ids.GenerateTestID(), 1), out(ids.GenerateTestID(), 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: errMultipleStakedAssets,
		},
		{
			name: "stake not sorted",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{out(assetID, 2), out(assetID, 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: errOutputsNotSorted,
		},
		{
			name: "stake overflow",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{out(assetID, math.MaxUint64), out(assetID, 2)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "weight mismatch",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 1}, ids.GenerateTestID(), []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: errDelegatorWeightMismatch,
		},
		{
			name: "valid net validator",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 2}, ids.GenerateTestID(), []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: nil,
		},
		{
			name: "valid primary network validator",
			build: func(t *testing.T) *AddPermissionlessDelegatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessDelegatorTx(validBase(), Validator{Wght: 2}, constants.PrimaryNetworkID, []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner())
				require.NoError(t, err)
				return tx
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.build(t).SyntacticVerify(rt), tt.err)
		})
	}
}

func TestAddPermissionlessDelegatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddPermissionlessDelegatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}

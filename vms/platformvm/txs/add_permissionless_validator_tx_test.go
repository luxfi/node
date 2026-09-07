// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/utxo/secp256k1fx"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	lux "github.com/luxfi/utxo"
)

// TestAddPermissionlessValidatorTx_RoundTrip exercises the struct-is-wire path
// for both a chain (non-primary, empty signer) and a primary-network validator
// (real BLS proof-of-possession): the delta fields round-trip through Parse.
// This replaces the deleted linearcodec golden-byte + JSON serialization tests.
func TestAddPermissionlessValidatorTx_RoundTrip(t *testing.T) {
	require := require.New(t)

	assetID := ids.GenerateTestID()
	stakeOuts := []*lux.TransferableOutput{
		{Asset: lux.Asset{ID: assetID}, Out: &secp256k1fx.TransferOutput{
			Amt:          3,
			OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		}},
	}
	valOwner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	delOwner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	vdr := Validator{NodeID: ids.GenerateTestNodeID(), Start: 1, End: 100, Wght: 3}

	// Chain (non-primary) validator: empty signer off the primary network.
	netTx, err := NewAddPermissionlessValidatorTx(spendBase(), vdr, ids.GenerateTestID(), &signer.Empty{}, stakeOuts, valOwner, delOwner, 100)
	require.NoError(err)
	gotNet := roundTrip(t, netTx).(*AddPermissionlessValidatorTx)
	require.Equal(vdr, gotNet.Validator())
	require.Equal(netTx.Chain(), gotNet.Chain())
	require.Equal(stakeOuts, gotNet.StakeOuts())
	require.Equal(valOwner, gotNet.ValidatorRewardsOwner())
	require.Equal(delOwner, gotNet.DelegatorRewardsOwner())
	require.EqualValues(100, gotNet.DelegationShares())
	require.Equal(&signer.Empty{}, gotNet.Signer())

	// Primary-network validator: real BLS proof-of-possession round-trips by
	// its wire bytes (the parsed PoP has no cached publicKey until Verify).
	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)
	primTx, err := NewAddPermissionlessValidatorTx(spendBase(), vdr, constants.PrimaryNetworkID, pop, stakeOuts, valOwner, delOwner, 100)
	require.NoError(err)
	gotPrim := roundTrip(t, primTx).(*AddPermissionlessValidatorTx)
	require.Equal(constants.PrimaryNetworkID, gotPrim.Chain())
	gotSig, ok := gotPrim.Signer().(*signer.ProofOfPossession)
	require.True(ok)
	require.Equal(pop.PublicKey, gotSig.PublicKey)
	require.Equal(pop.ProofOfPossession, gotSig.ProofOfPossession)
}

func TestAddPermissionlessValidatorTxSyntacticVerify(t *testing.T) {
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

	// Real BLS PoP for the "valid primary network validator" case.
	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)

	tests := []struct {
		name  string
		build func(t *testing.T) *AddPermissionlessValidatorTx
		err   error
	}{
		{
			name:  "nil tx",
			build: func(*testing.T) *AddPermissionlessValidatorTx { return nil },
			err:   ErrNilTx,
		},
		{
			name: "empty nodeID",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.EmptyNodeID}, ids.GenerateTestID(), &signer.Empty{}, nil, goodOwner(), goodOwner(), 0)
				require.NoError(t, err)
				return tx
			},
			err: errEmptyNodeID,
		},
		{
			name: "no provided stake",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID()}, ids.GenerateTestID(), &signer.Empty{}, nil, goodOwner(), goodOwner(), 0)
				require.NoError(t, err)
				return tx
			},
			err: errNoStake,
		},
		{
			name: "too many shares",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID()}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, goodOwner(), goodOwner(), reward.PercentDenominator+1)
				require.NoError(t, err)
				return tx
			},
			err: errTooManyShares,
		},
		{
			name: "invalid BaseTx",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(invalidBase(), Validator{NodeID: ids.GenerateTestNodeID()}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: lux.ErrWrongNetworkID,
		},
		{
			// Was: fxmock owner returns errCustom. Reproduced with a real
			// unspendable owner (Threshold > #Addrs) → ErrOutputUnspendable.
			name: "invalid rewards owner",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				badOwner := &secp256k1fx.OutputOwners{Threshold: 1}
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, badOwner, badOwner, reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "wrong signer",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, constants.PrimaryNetworkID, &signer.Empty{}, []*lux.TransferableOutput{out(ids.GenerateTestID(), 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: errInvalidSigner,
		},
		{
			// Was: luxmock stake-out Verify errCustom. Reproduced with a real
			// unspendable stake output → ErrOutputUnspendable.
			name: "invalid stake output",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				badOut := &lux.TransferableOutput{Asset: lux.Asset{ID: ids.GenerateTestID()}, Out: &secp256k1fx.TransferOutput{Amt: 1, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1}}}
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{badOut}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "stake overflow",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(assetID, math.MaxUint64), out(assetID, 2)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "multiple staked assets",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(ids.GenerateTestID(), 1), out(ids.GenerateTestID(), 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: errMultipleStakedAssets,
		},
		{
			name: "stake not sorted",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(assetID, 2), out(assetID, 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: errOutputsNotSorted,
		},
		{
			name: "weight mismatch",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 1}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: errValidatorWeightMismatch,
		},
		{
			name: "valid net validator",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 2}, ids.GenerateTestID(), &signer.Empty{}, []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
				require.NoError(t, err)
				return tx
			},
			err: nil,
		},
		{
			name: "valid primary network validator",
			build: func(t *testing.T) *AddPermissionlessValidatorTx {
				assetID := ids.GenerateTestID()
				tx, err := NewAddPermissionlessValidatorTx(validBase(), Validator{NodeID: ids.GenerateTestNodeID(), Wght: 2}, constants.PrimaryNetworkID, pop, []*lux.TransferableOutput{out(assetID, 1), out(assetID, 1)}, goodOwner(), goodOwner(), reward.PercentDenominator)
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

func TestAddPermissionlessValidatorTxNotDelegatorTx(t *testing.T) {
	txIntf := any((*AddPermissionlessValidatorTx)(nil))
	_, ok := txIntf.(DelegatorTx)
	require.False(t, ok)
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/reward"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestAddValidatorTxSyntacticVerify pins the AddValidatorTx invariants. Struct-
// is-wire has no post-hoc field mutation, so every (in)valid case is expressed
// by passing values THROUGH the NewAddValidatorTx constructor.
func TestAddValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	weight := uint64(2022)
	goodOwner := func() *secp256k1fx.OutputOwners {
		return &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{preFundedKeys[0].Address()}}
	}
	validator := func() Validator {
		return Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: weight}
	}
	stakeOut := func(assetID ids.ID, owner *secp256k1fx.OutputOwners) []*lux.TransferableOutput {
		return []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: assetID},
			Out:   &secp256k1fx.TransferOutput{Amt: weight, OutputOwners: *owner},
		}}
	}
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: signed tx is nil
	var stx *Tx
	require.ErrorIs(stx.SyntacticVerify(rt), ErrNilSignedTx)

	// Case: unsigned tx is nil
	var nilTx *AddValidatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: valid tx
	valid, err := NewAddValidatorTx(base(rt.NetworkID), validator(), stakeOut(rt.UTXOAssetID, goodOwner()), goodOwner(), reward.PercentDenominator)
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))

	// Case: wrong network ID
	wrongNet, err := NewAddValidatorTx(base(rt.NetworkID+1), validator(), stakeOut(rt.UTXOAssetID, goodOwner()), goodOwner(), reward.PercentDenominator)
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: stake owner has no addresses (unspendable stake output)
	stakeNoAddr, err := NewAddValidatorTx(base(rt.NetworkID), validator(), stakeOut(rt.UTXOAssetID, &secp256k1fx.OutputOwners{Threshold: 1}), goodOwner(), reward.PercentDenominator)
	require.NoError(err)
	require.ErrorIs(stakeNoAddr.SyntacticVerify(rt), secp256k1fx.ErrOutputUnspendable)

	// Case: rewards owner has no addresses (unspendable rewards owner)
	rewardsNoAddr, err := NewAddValidatorTx(base(rt.NetworkID), validator(), stakeOut(rt.UTXOAssetID, goodOwner()), &secp256k1fx.OutputOwners{Threshold: 1}, reward.PercentDenominator)
	require.NoError(err)
	require.ErrorIs(rewardsNoAddr.SyntacticVerify(rt), secp256k1fx.ErrOutputUnspendable)

	// Case: too many shares (1 more than max)
	tooManyShares, err := NewAddValidatorTx(base(rt.NetworkID), validator(), stakeOut(rt.UTXOAssetID, goodOwner()), goodOwner(), reward.PercentDenominator+1)
	require.NoError(err)
	require.ErrorIs(tooManyShares.SyntacticVerify(rt), errTooManyShares)
}

func TestAddValidatorTxSyntacticVerifyNotLUX(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	owner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{preFundedKeys[0].Address()}}
	weight := uint64(2022)
	validator := Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: weight}
	// Stake asset is not the primary-network UTXO asset.
	stakeOuts := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: ids.GenerateTestID()},
		Out:   &secp256k1fx.TransferOutput{Amt: weight, OutputOwners: *owner},
	}}

	u, err := NewAddValidatorTx(&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}, validator, stakeOuts, owner, reward.PercentDenominator)
	require.NoError(err)
	require.ErrorIs(u.SyntacticVerify(rt), errStakeMustBeLUX)
}

// NOTE: the codec-era TestAddValidatorTxNotDelegatorTx (asserting
// *AddValidatorTx does NOT satisfy DelegatorTx) is intentionally dropped. It
// exercised deleted behavior: under struct-is-wire, RewardsOwner changed from a
// struct FIELD to an accessor METHOD, and RewardsOwner() is the only member
// DelegatorTx needs beyond what AddValidatorTx already exposes (UnsignedTx +
// PermissionlessStaker). So *AddValidatorTx now structurally satisfies both
// ValidatorTx and DelegatorTx. This is harmless: the sole production dispatch
// (service.go getStakerAttributes) type-switches `case ValidatorTx` BEFORE
// `case DelegatorTx`, and Go evaluates cases in order, so an AddValidatorTx is
// always routed through the ValidatorTx branch (correct rewards/shares).

// TestAddValidatorTxSyntacticVerify_ArbitraryPrimaryNetworkIDs asserts that
// AddValidatorTx — the canonical post-LP-018 add-validator-to-a-network tx —
// accepts arbitrary primary networkIDs, not just the Lux primary values
// (1/2/3/1337). A sovereign L1 IS a primary network at its own networkID;
// downstream consumers may operate primary networks at any uint32 they choose.
// The tx body has no per-chain "Chain" field, so SyntacticVerify must succeed
// against any valid primary networkID.
func TestAddValidatorTxSyntacticVerify_ArbitraryPrimaryNetworkIDs(t *testing.T) {
	// Lux primaries (1/2/3/1337) plus four arbitrary synthetic IDs covering
	// low / mid / high uint32 ranges, to demonstrate the tx is networkID-
	// agnostic without baking any downstream value into this upstream test.
	primaryNetworkIDs := []uint32{
		1,    // Lux mainnet
		2,    // Lux testnet
		3,    // Lux local
		1337, // Lux dev/localnet
		9001,
		123456,
		7_777_777,
		4_000_000_000,
	}

	for _, networkID := range primaryNetworkIDs {
		t.Run(formatPrimaryNetworkID(networkID), func(t *testing.T) {
			require := require.New(t)

			rt := consensustest.Runtime(t, ids.GenerateTestID())
			rt.NetworkID = networkID // operate the primary network at an arbitrary ID

			weight := uint64(2026)
			owner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{preFundedKeys[0].Address()}}
			validator := Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: weight}
			stakeOuts := []*lux.TransferableOutput{{
				Asset: lux.Asset{ID: rt.UTXOAssetID},
				Out:   &secp256k1fx.TransferOutput{Amt: weight, OutputOwners: *owner},
			}}

			u, err := NewAddValidatorTx(&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}, validator, stakeOuts, owner, reward.PercentDenominator)
			require.NoError(err)
			require.NoError(u.SyntacticVerify(rt))
		})
	}
}

func formatPrimaryNetworkID(id uint32) string {
	switch id {
	case 1:
		return "mainnet-1"
	case 2:
		return "testnet-2"
	case 3:
		return "local-3"
	case 1337:
		return "dev-1337"
	default:
		return fmt.Sprintf("primary-networkID-%d", id)
	}
}

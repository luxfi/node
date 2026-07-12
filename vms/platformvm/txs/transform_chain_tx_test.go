// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/utxo/secp256k1fx"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/node/vms/platformvm/reward"
	lux "github.com/luxfi/utxo"
)

// TestTransformChainTx_RoundTrip exercises the struct-is-wire path: every delta
// field (chain, asset, the supply/rate/stake/duration knobs, the weight factor
// and uptime requirement, and the chain auth) round-trips through Parse. This
// replaces the deleted linearcodec golden-byte + JSON serialization tests.
func TestTransformChainTx_RoundTrip(t *testing.T) {
	require := require.New(t)

	chain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{0, 2, 5}}
	in, err := NewTransformChainTx(
		spendBase(), chain, assetID,
		1_000_000, // initial supply
		2_000_000, // maximum supply
		10_000,    // min consumption rate
		90_000,    // max consumption rate
		100,       // min validator stake
		5_000,     // max validator stake
		60,        // min stake duration
		3_600,     // max stake duration
		20_000,    // min delegation fee
		1,         // min delegator stake
		5,         // max validator weight factor
		80_000,    // uptime requirement
		auth,
	)
	require.NoError(err)

	got := roundTrip(t, in).(*TransformChainTx)
	require.Equal(chain, got.Chain())
	require.Equal(assetID, got.AssetID())
	require.EqualValues(1_000_000, got.InitialSupply())
	require.EqualValues(2_000_000, got.MaximumSupply())
	require.EqualValues(10_000, got.MinConsumptionRate())
	require.EqualValues(90_000, got.MaxConsumptionRate())
	require.EqualValues(100, got.MinValidatorStake())
	require.EqualValues(5_000, got.MaxValidatorStake())
	require.EqualValues(60, got.MinStakeDuration())
	require.EqualValues(3_600, got.MaxStakeDuration())
	require.EqualValues(20_000, got.MinDelegationFee())
	require.EqualValues(1, got.MinDelegatorStake())
	require.EqualValues(5, got.MaxValidatorWeightFactor())
	require.EqualValues(80_000, got.UptimeRequirement())

	gotAuth, ok := got.ChainAuth().(*secp256k1fx.Input)
	require.True(ok)
	require.Equal(auth.SigIndices, gotAuth.SigIndices)
}

// transformParams mirrors the NewTransformChainTx arguments so each error-path
// case can start from a fully-valid baseline and perturb exactly one field —
// struct-is-wire has no post-hoc mutation, so the bad value goes THROUGH the
// constructor and the SAME sentinel is asserted from SyntacticVerify.
type transformParams struct {
	base                     *lux.BaseTx
	chain                    ids.ID
	assetID                  ids.ID
	initialSupply            uint64
	maximumSupply            uint64
	minConsumptionRate       uint64
	maxConsumptionRate       uint64
	minValidatorStake        uint64
	maxValidatorStake        uint64
	minStakeDuration         uint32
	maxStakeDuration         uint32
	minDelegationFee         uint32
	minDelegatorStake        uint64
	maxValidatorWeightFactor byte
	uptimeRequirement        uint32
	chainAuth                *secp256k1fx.Input
}

func TestTransformChainTxSyntacticVerify(t *testing.T) {
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	validBase := func() *lux.BaseTx {
		return &lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}
	}
	invalidBase := func() *lux.BaseTx {
		return &lux.BaseTx{NetworkID: 0, BlockchainID: rt.ChainID} // wrong networkID
	}
	// A fully-valid baseline: every field passes its own SyntacticVerify check.
	valid := func() transformParams {
		return transformParams{
			base:                     validBase(),
			chain:                    ids.GenerateTestID(),
			assetID:                  ids.GenerateTestID(),
			initialSupply:            10,
			maximumSupply:            10,
			minConsumptionRate:       0,
			maxConsumptionRate:       reward.PercentDenominator,
			minValidatorStake:        2,
			maxValidatorStake:        10,
			minStakeDuration:         1,
			maxStakeDuration:         2,
			minDelegationFee:         reward.PercentDenominator,
			minDelegatorStake:        1,
			maxValidatorWeightFactor: 1,
			uptimeRequirement:        reward.PercentDenominator,
			chainAuth:                &secp256k1fx.Input{SigIndices: []uint32{0}},
		}
	}
	mk := func(t *testing.T, p transformParams) *TransformChainTx {
		tx, err := NewTransformChainTx(
			p.base, p.chain, p.assetID,
			p.initialSupply, p.maximumSupply,
			p.minConsumptionRate, p.maxConsumptionRate,
			p.minValidatorStake, p.maxValidatorStake,
			p.minStakeDuration, p.maxStakeDuration,
			p.minDelegationFee, p.minDelegatorStake,
			p.maxValidatorWeightFactor, p.uptimeRequirement,
			p.chainAuth,
		)
		require.NoError(t, err)
		return tx
	}

	tests := []struct {
		name  string
		build func(t *testing.T) *TransformChainTx
		err   error
	}{
		{
			name:  "nil tx",
			build: func(*testing.T) *TransformChainTx { return nil },
			err:   ErrNilTx,
		},
		{
			name: "invalid netID",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.chain = constants.PrimaryNetworkID
				return mk(t, p)
			},
			err: errCantTransformPrimaryNetwork,
		},
		{
			name: "empty assetID",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.assetID = ids.Empty
				return mk(t, p)
			},
			err: errEmptyAssetID,
		},
		{
			name: "LUX assetID",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.assetID = rt.UTXOAssetID
				return mk(t, p)
			},
			err: errAssetIDCantBeLUX,
		},
		{
			name: "initialSupply == 0",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.initialSupply = 0
				return mk(t, p)
			},
			err: errInitialSupplyZero,
		},
		{
			name: "initialSupply > maximumSupply",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.initialSupply = 2
				p.maximumSupply = 1
				return mk(t, p)
			},
			err: errInitialSupplyGreaterThanMaxSupply,
		},
		{
			name: "minConsumptionRate > maxConsumptionRate",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minConsumptionRate = 2
				p.maxConsumptionRate = 1
				return mk(t, p)
			},
			err: errMinConsumptionRateTooLarge,
		},
		{
			name: "maxConsumptionRate > 100%",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minConsumptionRate = 0
				p.maxConsumptionRate = reward.PercentDenominator + 1
				return mk(t, p)
			},
			err: errMaxConsumptionRateTooLarge,
		},
		{
			name: "minValidatorStake == 0",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minValidatorStake = 0
				return mk(t, p)
			},
			err: errMinValidatorStakeZero,
		},
		{
			name: "minValidatorStake > initialSupply",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.initialSupply = 1
				p.minValidatorStake = 2
				return mk(t, p)
			},
			err: errMinValidatorStakeAboveSupply,
		},
		{
			name: "minValidatorStake > maxValidatorStake",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minValidatorStake = 2
				p.maxValidatorStake = 1
				return mk(t, p)
			},
			err: errMinValidatorStakeAboveMax,
		},
		{
			name: "maxValidatorStake > maximumSupply",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.maxValidatorStake = 11
				return mk(t, p)
			},
			err: errMaxValidatorStakeTooLarge,
		},
		{
			name: "minStakeDuration == 0",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minStakeDuration = 0
				return mk(t, p)
			},
			err: errMinStakeDurationZero,
		},
		{
			name: "minStakeDuration > maxStakeDuration",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minStakeDuration = 2
				p.maxStakeDuration = 1
				return mk(t, p)
			},
			err: errMinStakeDurationTooLarge,
		},
		{
			name: "minDelegationFee > 100%",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minDelegationFee = reward.PercentDenominator + 1
				return mk(t, p)
			},
			err: errMinDelegationFeeTooLarge,
		},
		{
			name: "minDelegatorStake == 0",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.minDelegatorStake = 0
				return mk(t, p)
			},
			err: errMinDelegatorStakeZero,
		},
		{
			name: "maxValidatorWeightFactor == 0",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.maxValidatorWeightFactor = 0
				return mk(t, p)
			},
			err: errMaxValidatorWeightFactorZero,
		},
		{
			name: "uptimeRequirement > 100%",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.uptimeRequirement = reward.PercentDenominator + 1
				return mk(t, p)
			},
			err: errUptimeRequirementTooLarge,
		},
		{
			// Was: verifymock chainAuth returns errInvalidNetAuth. Reproduced
			// with a real *secp256k1fx.Input whose indices are not sorted-unique.
			name: "invalid chainAuth",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.chainAuth = &secp256k1fx.Input{SigIndices: []uint32{1, 0}}
				return mk(t, p)
			},
			err: secp256k1fx.ErrInputIndicesNotSortedUnique,
		},
		{
			name: "invalid BaseTx",
			build: func(t *testing.T) *TransformChainTx {
				p := valid()
				p.base = invalidBase()
				return mk(t, p)
			},
			err: lux.ErrWrongNetworkID,
		},
		{
			name: "passes verification",
			build: func(t *testing.T) *TransformChainTx {
				return mk(t, valid())
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

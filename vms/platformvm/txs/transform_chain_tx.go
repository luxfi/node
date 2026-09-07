// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*TransformChainTx)(nil)

	errCantTransformPrimaryNetwork       = errors.New("cannot transform primary network")
	errEmptyAssetID                      = errors.New("empty asset ID is not valid")
	errAssetIDCantBeLUX                  = errors.New("asset ID can't be LUX")
	errInitialSupplyZero                 = errors.New("initial supply must be non-0")
	errInitialSupplyGreaterThanMaxSupply = errors.New("initial supply can't be greater than maximum supply")
	errMinConsumptionRateTooLarge        = errors.New("min consumption rate must be less than or equal to max consumption rate")
	errMaxConsumptionRateTooLarge        = fmt.Errorf("max consumption rate must be less than or equal to %d", reward.PercentDenominator)
	errMinValidatorStakeZero             = errors.New("min validator stake must be non-0")
	errMinValidatorStakeAboveSupply      = errors.New("min validator stake must be less than or equal to initial supply")
	errMinValidatorStakeAboveMax         = errors.New("min validator stake must be less than or equal to max validator stake")
	errMaxValidatorStakeTooLarge         = errors.New("max validator stake must be less than or equal to max supply")
	errMinStakeDurationZero              = errors.New("min stake duration must be non-0")
	errMinStakeDurationTooLarge          = errors.New("min stake duration must be less than or equal to max stake duration")
	errMinDelegationFeeTooLarge          = fmt.Errorf("min delegation fee must be less than or equal to %d", reward.PercentDenominator)
	errMinDelegatorStakeZero             = errors.New("min delegator stake must be non-0")
	errMaxValidatorWeightFactorZero      = errors.New("max validator weight factor must be non-0")
	errUptimeRequirementTooLarge         = fmt.Errorf("uptime requirement must be less than or equal to %d", reward.PercentDenominator)
)

// TransformChainTx transforms a chain into a permissionless one. The struct IS
// the wire: it embeds the spending envelope and reads its delta fields by
// offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	Chain                    32B @ 77
//	AssetID                  32B @ 109
//	InitialSupply            u64 @ 141
//	MaximumSupply            u64 @ 149
//	MinConsumptionRate       u64 @ 157
//	MaxConsumptionRate       u64 @ 165
//	MinValidatorStake        u64 @ 173
//	MaxValidatorStake        u64 @ 181
//	MinStakeDuration         u32 @ 189
//	MaxStakeDuration         u32 @ 193
//	MinDelegationFee         u32 @ 197
//	MinDelegatorStake        u64 @ 201
//	MaxValidatorWeightFactor u8  @ 209
//	UptimeRequirement        u32 @ 210
//	ChainAuth                8B  @ 214  (auth ptr: sig-index list)
type TransformChainTx struct {
	spendingTx
}

const (
	offTransformChain                    = spendSize // 77
	offTransformAssetID                  = 109
	offTransformInitialSupply            = 141
	offTransformMaximumSupply            = 149
	offTransformMinConsumptionRate       = 157
	offTransformMaxConsumptionRate       = 165
	offTransformMinValidatorStake        = 173
	offTransformMaxValidatorStake        = 181
	offTransformMinStakeDuration         = 189
	offTransformMaxStakeDuration         = 193
	offTransformMinDelegationFee         = 197
	offTransformMinDelegatorStake        = 201
	offTransformMaxValidatorWeightFactor = 209
	offTransformUptimeRequirement        = 210
	offTransformChainAuth                = 214
	sizeTransformChain                   = 222
)

// NewTransformChainTx builds the tx into a fresh zap buffer.
func NewTransformChainTx(
	base *lux.BaseTx,
	chain ids.ID,
	assetID ids.ID,
	initialSupply uint64,
	maximumSupply uint64,
	minConsumptionRate uint64,
	maxConsumptionRate uint64,
	minValidatorStake uint64,
	maxValidatorStake uint64,
	minStakeDuration uint32,
	maxStakeDuration uint32,
	minDelegationFee uint32,
	minDelegatorStake uint64,
	maxValidatorWeightFactor byte,
	uptimeRequirement uint32,
	chainAuth verify.Verifiable,
) (*TransformChainTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeTransformChain)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeTransformChain)
	setEnvelope(ob, kindTransformChain, base, p)
	setID(ob, offTransformChain, chain)
	setID(ob, offTransformAssetID, assetID)
	ob.SetUint64(offTransformInitialSupply, initialSupply)
	ob.SetUint64(offTransformMaximumSupply, maximumSupply)
	ob.SetUint64(offTransformMinConsumptionRate, minConsumptionRate)
	ob.SetUint64(offTransformMaxConsumptionRate, maxConsumptionRate)
	ob.SetUint64(offTransformMinValidatorStake, minValidatorStake)
	ob.SetUint64(offTransformMaxValidatorStake, maxValidatorStake)
	ob.SetUint32(offTransformMinStakeDuration, minStakeDuration)
	ob.SetUint32(offTransformMaxStakeDuration, maxStakeDuration)
	ob.SetUint32(offTransformMinDelegationFee, minDelegationFee)
	ob.SetUint64(offTransformMinDelegatorStake, minDelegatorStake)
	ob.SetUint8(offTransformMaxValidatorWeightFactor, maxValidatorWeightFactor)
	ob.SetUint32(offTransformUptimeRequirement, uptimeRequirement)
	ob.SetList(offTransformChainAuth, authOff, authCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &TransformChainTx{spendingTx{msg}}, nil
}

// Chain is the ID of the chain to transform (offset read).
func (tx *TransformChainTx) Chain() ids.ID { return readID(tx.root(), offTransformChain) }

// AssetID is the asset to use when staking on the chain.
func (tx *TransformChainTx) AssetID() ids.ID { return readID(tx.root(), offTransformAssetID) }

// InitialSupply is the amount to initially specify as the current supply.
func (tx *TransformChainTx) InitialSupply() uint64 {
	return tx.root().Uint64(offTransformInitialSupply)
}

// MaximumSupply is the amount to specify as the maximum token supply.
func (tx *TransformChainTx) MaximumSupply() uint64 {
	return tx.root().Uint64(offTransformMaximumSupply)
}

// MinConsumptionRate is the rate to allocate funds if the validator's stake
// duration is 0.
func (tx *TransformChainTx) MinConsumptionRate() uint64 {
	return tx.root().Uint64(offTransformMinConsumptionRate)
}

// MaxConsumptionRate is the rate to allocate funds if the validator's stake
// duration is equal to the minting period.
func (tx *TransformChainTx) MaxConsumptionRate() uint64 {
	return tx.root().Uint64(offTransformMaxConsumptionRate)
}

// MinValidatorStake is the minimum amount of funds required to become a
// validator.
func (tx *TransformChainTx) MinValidatorStake() uint64 {
	return tx.root().Uint64(offTransformMinValidatorStake)
}

// MaxValidatorStake is the maximum amount of funds a single validator can be
// allocated, including delegated funds.
func (tx *TransformChainTx) MaxValidatorStake() uint64 {
	return tx.root().Uint64(offTransformMaxValidatorStake)
}

// MinStakeDuration is the minimum number of seconds a staker can stake for.
func (tx *TransformChainTx) MinStakeDuration() uint32 {
	return tx.root().Uint32(offTransformMinStakeDuration)
}

// MaxStakeDuration is the maximum number of seconds a staker can stake for.
func (tx *TransformChainTx) MaxStakeDuration() uint32 {
	return tx.root().Uint32(offTransformMaxStakeDuration)
}

// MinDelegationFee is the minimum percentage a validator must charge a
// delegator for delegating.
func (tx *TransformChainTx) MinDelegationFee() uint32 {
	return tx.root().Uint32(offTransformMinDelegationFee)
}

// MinDelegatorStake is the minimum amount of funds required to become a
// delegator.
func (tx *TransformChainTx) MinDelegatorStake() uint64 {
	return tx.root().Uint64(offTransformMinDelegatorStake)
}

// MaxValidatorWeightFactor is the factor which calculates the maximum amount of
// delegation a validator can receive.
func (tx *TransformChainTx) MaxValidatorWeightFactor() byte {
	return tx.root().Uint8(offTransformMaxValidatorWeightFactor)
}

// UptimeRequirement is the minimum percentage a validator must be online and
// responsive to receive a reward.
func (tx *TransformChainTx) UptimeRequirement() uint32 {
	return tx.root().Uint32(offTransformUptimeRequirement)
}

// ChainAuth authorizes this transformation.
func (tx *TransformChainTx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offTransformChainAuth)
}

func (tx *TransformChainTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Chain() == constants.PrimaryNetworkID:
		return errCantTransformPrimaryNetwork
	case tx.AssetID() == ids.Empty:
		return errEmptyAssetID
	case tx.AssetID() == rt.UTXOAssetID:
		return errAssetIDCantBeLUX
	case tx.InitialSupply() == 0:
		return errInitialSupplyZero
	case tx.InitialSupply() > tx.MaximumSupply():
		return errInitialSupplyGreaterThanMaxSupply
	case tx.MinConsumptionRate() > tx.MaxConsumptionRate():
		return errMinConsumptionRateTooLarge
	case tx.MaxConsumptionRate() > reward.PercentDenominator:
		return errMaxConsumptionRateTooLarge
	case tx.MinValidatorStake() == 0:
		return errMinValidatorStakeZero
	case tx.MinValidatorStake() > tx.InitialSupply():
		return errMinValidatorStakeAboveSupply
	case tx.MinValidatorStake() > tx.MaxValidatorStake():
		return errMinValidatorStakeAboveMax
	case tx.MaxValidatorStake() > tx.MaximumSupply():
		return errMaxValidatorStakeTooLarge
	case tx.MinStakeDuration() == 0:
		return errMinStakeDurationZero
	case tx.MinStakeDuration() > tx.MaxStakeDuration():
		return errMinStakeDurationTooLarge
	case tx.MinDelegationFee() > reward.PercentDenominator:
		return errMinDelegationFeeTooLarge
	case tx.MinDelegatorStake() == 0:
		return errMinDelegatorStakeZero
	case tx.MaxValidatorWeightFactor() == 0:
		return errMaxValidatorWeightFactorZero
	case tx.UptimeRequirement() > reward.PercentDenominator:
		return errUptimeRequirementTooLarge
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return tx.ChainAuth().Verify()
}

func (tx *TransformChainTx) Visit(visitor Visitor) error {
	return visitor.TransformChainTx(tx)
}

// Initialize is a no-op; the struct is already the wire.
func (tx *TransformChainTx) Initialize(ctx context.Context) error { return nil }

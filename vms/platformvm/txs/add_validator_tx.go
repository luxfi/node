// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	bls "github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx      = (*AddValidatorTx)(nil)
	_ ValidatorTx     = (*AddValidatorTx)(nil)
	_ ScheduledStaker = (*AddValidatorTx)(nil)

	errTooManyShares = fmt.Errorf("a staker can only require at most %d shares from delegators", reward.PercentDenominator)
)

// AddValidatorTx is an unsigned addValidatorTx. The struct IS the wire: it
// holds the zap buffer and reads its fields by offset. No codec, no marshal.
//
// Wire: zap header + object{ envelope@0..76, Validator@77, StakeOuts@121,
// RewardsOwner@137, DelegationShares@157 }.
type AddValidatorTx struct {
	spendingTx
}

const (
	offAVValidator        = spendSize                      // 77: inline Validator (44B)
	offAVStakeOuts        = offAVValidator + validatorSize // 121: stake-outs list ptr (8B)
	offAVStakeAddrs       = offAVStakeOuts + 8             // 129: stake-outs owner-addr array ptr (8B)
	offAVRewardsThreshold = offAVStakeAddrs + 8            // 137: rewards owner threshold (u32)
	offAVRewardsLocktime  = offAVRewardsThreshold + 4      // 141: rewards owner locktime (u64)
	offAVRewardsAddrs     = offAVRewardsLocktime + 8       // 149: rewards owner addr array ptr (8B)
	offAVDelegationShares = offAVRewardsAddrs + 8          // 157: delegation shares (u32)
	addValidatorSize      = offAVDelegationShares + 4      // 161
)

// NewAddValidatorTx builds the tx into a fresh zap buffer.
func NewAddValidatorTx(base *lux.BaseTx, validator Validator, stakeOuts []*lux.TransferableOutput, rewardsOwner fx.Owner, delegationShares uint32) (*AddValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + addValidatorSize)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	stakeListOff, stakeListCount, stakeAddrOff, stakeAddrCount, err := writeExtraOuts(b, stakeOuts)
	if err != nil {
		return nil, err
	}
	ownerThreshold, ownerLocktime, ownerAddrOff, ownerAddrCount, err := writeOwner(b, rewardsOwner)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(addValidatorSize)
	setEnvelope(ob, kindAddValidator, base, p)
	setValidator(ob, offAVValidator, validator)
	ob.SetList(offAVStakeOuts, stakeListOff, stakeListCount)
	ob.SetList(offAVStakeAddrs, stakeAddrOff, stakeAddrCount)
	setOwner(ob, offAVRewardsThreshold, offAVRewardsLocktime, offAVRewardsAddrs, ownerThreshold, ownerLocktime, ownerAddrOff, ownerAddrCount)
	ob.SetUint32(offAVDelegationShares, delegationShares)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &AddValidatorTx{spendingTx{msg}}, nil
}

// Validator describes the delegatee (offset read).
func (tx *AddValidatorTx) Validator() Validator { return readValidator(tx.root(), offAVValidator) }

// StakeOuts is where staked tokens go when done validating (offset read).
func (tx *AddValidatorTx) StakeOuts() []*lux.TransferableOutput {
	return readExtraOuts(tx.root(), offAVStakeOuts, offAVStakeAddrs)
}

// RewardsOwner is where staking rewards go when done validating.
func (tx *AddValidatorTx) RewardsOwner() fx.Owner {
	return readOwner(tx.root(), offAVRewardsThreshold, offAVRewardsLocktime, offAVRewardsAddrs)
}

// DelegationShares is the fee (times 10,000) charged to delegators.
func (tx *AddValidatorTx) DelegationShares() uint32 { return tx.root().Uint32(offAVDelegationShares) }

// ---- Staker / ValidatorTx / ScheduledStaker interface ----

func (*AddValidatorTx) ChainID() ids.ID {
	return constants.PrimaryNetworkID
}

func (tx *AddValidatorTx) NodeID() ids.NodeID {
	return tx.Validator().NodeID
}

func (*AddValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (tx *AddValidatorTx) StartTime() time.Time {
	return time.Unix(int64(tx.Validator().Start), 0)
}

func (tx *AddValidatorTx) EndTime() time.Time {
	return time.Unix(int64(tx.Validator().End), 0)
}

func (tx *AddValidatorTx) Weight() uint64 {
	return tx.Validator().Wght
}

func (*AddValidatorTx) PendingPriority() Priority {
	return PrimaryNetworkValidatorPendingPriority
}

func (*AddValidatorTx) CurrentPriority() Priority {
	return PrimaryNetworkValidatorCurrentPriority
}

// Stake is where staked tokens go when done validating, with FxID set on
// each output (secp256k1fx) as the legacy InitRuntime did.
func (tx *AddValidatorTx) Stake() []*lux.TransferableOutput {
	outs := tx.StakeOuts()
	for _, out := range outs {
		out.FxID = secp256k1fx.ID
	}
	return outs
}

func (tx *AddValidatorTx) ValidationRewardsOwner() fx.Owner {
	return tx.RewardsOwner()
}

func (tx *AddValidatorTx) DelegationRewardsOwner() fx.Owner {
	return tx.RewardsOwner()
}

func (tx *AddValidatorTx) Shares() uint32 {
	return tx.DelegationShares()
}

// SyntacticVerify returns nil iff [tx] is valid.
func (tx *AddValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	if tx.DelegationShares() > reward.PercentDenominator { // shares in the allowed amount
		return errTooManyShares
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	v := tx.Validator()
	if err := verify.All(&v, tx.RewardsOwner()); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	stakeOuts := tx.StakeOuts()
	totalStakeWeight := uint64(0)
	for _, out := range stakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify output: %w", err)
		}
		newWeight, err := safemath.Add(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight

		assetID := out.AssetID()
		utxoAssetID := rt.UTXOAssetID
		if assetID != utxoAssetID {
			return fmt.Errorf("%w but is %q", errStakeMustBeLUX, assetID)
		}
	}

	switch {
	case !lux.IsSortedTransferableOutputs(stakeOuts):
		return errOutputsNotSorted
	case totalStakeWeight != v.Wght:
		return fmt.Errorf("%w: weight %d != stake %d", errValidatorWeightMismatch, v.Wght, totalStakeWeight)
	}
	return nil
}

func (tx *AddValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddValidatorTx(tx)
}

// Initialize is a no-op; Runtime is passed explicitly to InitRuntime.
func (tx *AddValidatorTx) Initialize(ctx context.Context) error {
	return nil
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	bls "github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx      = (*AddDelegatorTx)(nil)
	_ DelegatorTx     = (*AddDelegatorTx)(nil)
	_ ScheduledStaker = (*AddDelegatorTx)(nil)

	errDelegatorWeightMismatch = errors.New("delegator weight is not equal to total stake weight")
	errStakeMustBeLUX          = errors.New("stake must be LUX")
)

// AddDelegatorTx is an unsigned addDelegatorTx. The struct IS the wire: it
// holds the zap buffer and reads its fields by offset. No codec, no marshal.
//
// Wire: zap header + object{ envelope@0..76, Validator@77, StakeOuts@121,
// DelegationRewardsOwner@137 }.
type AddDelegatorTx struct {
	spendingTx
}

const (
	offADValidator        = spendSize                      // 77: inline Validator (44B)
	offADStakeOuts        = offADValidator + validatorSize // 121: stake-outs list ptr (8B)
	offADStakeAddrs       = offADStakeOuts + 8             // 129: stake-outs owner-addr array ptr (8B)
	offADRewardsThreshold = offADStakeAddrs + 8            // 137: rewards owner threshold (u32)
	offADRewardsLocktime  = offADRewardsThreshold + 4      // 141: rewards owner locktime (u64)
	offADRewardsAddrs     = offADRewardsLocktime + 8       // 149: rewards owner addr array ptr (8B)
	addDelegatorSize      = offADRewardsAddrs + 8          // 157
)

// NewAddDelegatorTx builds the tx into a fresh zap buffer.
func NewAddDelegatorTx(base *lux.BaseTx, validator Validator, stakeOuts []*lux.TransferableOutput, delegationRewardsOwner fx.Owner) (*AddDelegatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + addDelegatorSize)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	stakeListOff, stakeListCount, stakeAddrOff, stakeAddrCount, err := writeExtraOuts(b, stakeOuts)
	if err != nil {
		return nil, err
	}
	ownerThreshold, ownerLocktime, ownerAddrOff, ownerAddrCount, err := writeOwner(b, delegationRewardsOwner)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(addDelegatorSize)
	setEnvelope(ob, kindAddDelegator, base, p)
	setValidator(ob, offADValidator, validator)
	ob.SetList(offADStakeOuts, stakeListOff, stakeListCount)
	ob.SetList(offADStakeAddrs, stakeAddrOff, stakeAddrCount)
	setOwner(ob, offADRewardsThreshold, offADRewardsLocktime, offADRewardsAddrs, ownerThreshold, ownerLocktime, ownerAddrOff, ownerAddrCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &AddDelegatorTx{spendingTx{msg}}, nil
}

// Validator describes the delegatee (offset read).
func (tx *AddDelegatorTx) Validator() Validator { return readValidator(tx.root(), offADValidator) }

// StakeOuts is where staked tokens go when done validating (offset read).
func (tx *AddDelegatorTx) StakeOuts() []*lux.TransferableOutput {
	return readExtraOuts(tx.root(), offADStakeOuts, offADStakeAddrs)
}

// DelegationRewardsOwner is where staking rewards go when done validating.
func (tx *AddDelegatorTx) DelegationRewardsOwner() fx.Owner {
	return readOwner(tx.root(), offADRewardsThreshold, offADRewardsLocktime, offADRewardsAddrs)
}

// ---- Staker / DelegatorTx / ScheduledStaker interface ----

func (*AddDelegatorTx) ChainID() ids.ID {
	return constants.PrimaryNetworkID
}

func (tx *AddDelegatorTx) NodeID() ids.NodeID {
	return tx.Validator().NodeID
}

func (*AddDelegatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (tx *AddDelegatorTx) StartTime() time.Time {
	return time.Unix(int64(tx.Validator().Start), 0)
}

func (tx *AddDelegatorTx) EndTime() time.Time {
	return time.Unix(int64(tx.Validator().End), 0)
}

func (tx *AddDelegatorTx) Weight() uint64 {
	return tx.Validator().Wght
}

func (*AddDelegatorTx) PendingPriority() Priority {
	return PrimaryNetworkDelegatorLegacyPendingPriority
}

func (*AddDelegatorTx) CurrentPriority() Priority {
	return PrimaryNetworkDelegatorCurrentPriority
}

// Stake is where staked tokens go when done validating, with FxID set on
// each output (secp256k1fx) as the legacy InitRuntime did.
func (tx *AddDelegatorTx) Stake() []*lux.TransferableOutput {
	outs := tx.StakeOuts()
	for _, out := range outs {
		out.FxID = secp256k1fx.ID
	}
	return outs
}

func (tx *AddDelegatorTx) RewardsOwner() fx.Owner {
	return tx.DelegationRewardsOwner()
}

// SyntacticVerify returns nil iff [tx] is valid.
func (tx *AddDelegatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	v := tx.Validator()
	if err := verify.All(&v, tx.DelegationRewardsOwner()); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	stakeOuts := tx.StakeOuts()
	totalStakeWeight := uint64(0)
	for _, out := range stakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output verification failed: %w", err)
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
		return fmt.Errorf("%w, delegator weight %d total stake weight %d",
			errDelegatorWeightMismatch,
			v.Wght,
			totalStakeWeight,
		)
	}
	return nil
}

func (tx *AddDelegatorTx) Visit(visitor Visitor) error {
	return visitor.AddDelegatorTx(tx)
}

// Initialize is a no-op; Runtime is passed explicitly to InitRuntime.
func (tx *AddDelegatorTx) Initialize(ctx context.Context) error {
	return nil
}

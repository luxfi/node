// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"fmt"

	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*AddPermissionlessDelegatorTx)(nil)

// AddPermissionlessDelegatorTx is an unsigned addPermissionlessDelegatorTx.
// The struct IS the wire: it holds the zap buffer and reads its fields by
// offset. No codec, no marshal.
//
// Wire: zap header + object{ envelope@0..76, Validator@77, Chain@121,
// StakeOuts@153, DelegationRewardsOwner@169 }.
type AddPermissionlessDelegatorTx struct {
	spendingTx
}

const (
	offAPDValidator                = spendSize                       // 77: inline Validator (44B)
	offAPDChain                    = offAPDValidator + validatorSize // 121: chain id (32B)
	offAPDStakeOuts                = offAPDChain + 32                // 153: stake-outs list ptr (8B)
	offAPDStakeAddrs               = offAPDStakeOuts + 8             // 161: stake-outs owner-addr array ptr (8B)
	offAPDRewardsThreshold         = offAPDStakeAddrs + 8            // 169: rewards owner threshold (u32)
	offAPDRewardsLocktime          = offAPDRewardsThreshold + 4      // 173: rewards owner locktime (u64)
	offAPDRewardsAddrs             = offAPDRewardsLocktime + 8       // 181: rewards owner addr array ptr (8B)
	addPermissionlessDelegatorSize = offAPDRewardsAddrs + 8          // 189
)

// NewAddPermissionlessDelegatorTx builds the tx into a fresh zap buffer.
func NewAddPermissionlessDelegatorTx(base *lux.BaseTx, validator Validator, chain ids.ID, stakeOuts []*lux.TransferableOutput, delegationRewardsOwner fx.Owner) (*AddPermissionlessDelegatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + addPermissionlessDelegatorSize)
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

	ob := b.StartObject(addPermissionlessDelegatorSize)
	setEnvelope(ob, kindAddPermissionlessDelegator, base, p)
	setValidator(ob, offAPDValidator, validator)
	setID(ob, offAPDChain, chain)
	ob.SetList(offAPDStakeOuts, stakeListOff, stakeListCount)
	ob.SetList(offAPDStakeAddrs, stakeAddrOff, stakeAddrCount)
	setOwner(ob, offAPDRewardsThreshold, offAPDRewardsLocktime, offAPDRewardsAddrs, ownerThreshold, ownerLocktime, ownerAddrOff, ownerAddrCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &AddPermissionlessDelegatorTx{spendingTx{msg}}, nil
}

// Validator describes the validator being delegated to (offset read).
func (tx *AddPermissionlessDelegatorTx) Validator() Validator {
	return readValidator(tx.root(), offAPDValidator)
}

// Chain is the id of the chain this validator is validating (offset read).
func (tx *AddPermissionlessDelegatorTx) Chain() ids.ID { return readID(tx.root(), offAPDChain) }

// StakeOuts is where staked tokens go when done validating (offset read).
func (tx *AddPermissionlessDelegatorTx) StakeOuts() []*lux.TransferableOutput {
	return readExtraOuts(tx.root(), offAPDStakeOuts, offAPDStakeAddrs)
}

// DelegationRewardsOwner is where staking rewards go when done validating.
func (tx *AddPermissionlessDelegatorTx) DelegationRewardsOwner() fx.Owner {
	return readOwner(tx.root(), offAPDRewardsThreshold, offAPDRewardsLocktime, offAPDRewardsAddrs)
}

// SyntacticVerify returns nil iff [tx] is valid.
func (tx *AddPermissionlessDelegatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	stakeOuts := tx.StakeOuts()
	if len(stakeOuts) == 0 { // Ensure there is provided stake
		return errNoStake
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	v := tx.Validator()
	if err := verify.All(&v, tx.DelegationRewardsOwner()); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	for _, out := range stakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify output: %w", err)
		}
	}

	firstStakeOutput := stakeOuts[0]
	stakedAssetID := firstStakeOutput.AssetID()
	totalStakeWeight := firstStakeOutput.Output().Amount()
	for _, out := range stakeOuts[1:] {
		newWeight, err := safemath.Add(totalStakeWeight, out.Output().Amount())
		if err != nil {
			return err
		}
		totalStakeWeight = newWeight

		assetID := out.AssetID()
		if assetID != stakedAssetID {
			return fmt.Errorf("%w: %q and %q", errMultipleStakedAssets, stakedAssetID, assetID)
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

func (tx *AddPermissionlessDelegatorTx) Visit(visitor Visitor) error {
	return visitor.AddPermissionlessDelegatorTx(tx)
}

// Initialize is a no-op; Runtime is passed explicitly to InitRuntime.
func (tx *AddPermissionlessDelegatorTx) Initialize(ctx context.Context) error {
	return nil
}

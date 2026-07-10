// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*AddPermissionlessValidatorTx)(nil)

	errEmptyNodeID             = errors.New("validator nodeID cannot be empty")
	errNoStake                 = errors.New("no stake")
	errInvalidSigner           = errors.New("invalid signer")
	errMultipleStakedAssets    = errors.New("multiple staked assets")
	errValidatorWeightMismatch = errors.New("validator weight mismatch")
)

// AddPermissionlessValidatorTx is an unsigned addPermissionlessValidatorTx.
// The struct IS the wire: it holds the zap buffer and reads its fields by
// offset. No codec, no marshal.
//
// Wire: zap header + object{ envelope@0..76, Validator@77, Chain@121,
// Signer@153, StakeOuts@298, ValidatorRewardsOwner@314,
// DelegatorRewardsOwner@334, DelegationShares@354 }.
type AddPermissionlessValidatorTx struct {
	spendingTx
}

const (
	offAPVValidator                = spendSize                       // 77: inline Validator (44B)
	offAPVChain                    = offAPVValidator + validatorSize // 121: chain id (32B)
	offAPVSigner                   = offAPVChain + 32                // 153: inline Signer (145B)
	offAPVStakeOuts                = offAPVSigner + signerSize       // 298: stake-outs list ptr (8B)
	offAPVStakeAddrs               = offAPVStakeOuts + 8             // 306: stake-outs owner-addr array ptr (8B)
	offAPVValRewardsThreshold      = offAPVStakeAddrs + 8            // 314: validator rewards owner threshold (u32)
	offAPVValRewardsLocktime       = offAPVValRewardsThreshold + 4   // 318: validator rewards owner locktime (u64)
	offAPVValRewardsAddrs          = offAPVValRewardsLocktime + 8    // 326: validator rewards owner addr array ptr (8B)
	offAPVDelRewardsThreshold      = offAPVValRewardsAddrs + 8       // 334: delegator rewards owner threshold (u32)
	offAPVDelRewardsLocktime       = offAPVDelRewardsThreshold + 4   // 338: delegator rewards owner locktime (u64)
	offAPVDelRewardsAddrs          = offAPVDelRewardsLocktime + 8    // 346: delegator rewards owner addr array ptr (8B)
	offAPVDelegationShares         = offAPVDelRewardsAddrs + 8       // 354: delegation shares (u32)
	addPermissionlessValidatorSize = offAPVDelegationShares + 4      // 358
)

// NewAddPermissionlessValidatorTx builds the tx into a fresh zap buffer.
func NewAddPermissionlessValidatorTx(base *lux.BaseTx, validator Validator, chain ids.ID, sig signer.Signer, stakeOuts []*lux.TransferableOutput, validatorRewardsOwner fx.Owner, delegatorRewardsOwner fx.Owner, delegationShares uint32) (*AddPermissionlessValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + addPermissionlessValidatorSize)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	stakeListOff, stakeListCount, stakeAddrOff, stakeAddrCount, err := writeExtraOuts(b, stakeOuts)
	if err != nil {
		return nil, err
	}
	valThreshold, valLocktime, valAddrOff, valAddrCount, err := writeOwner(b, validatorRewardsOwner)
	if err != nil {
		return nil, err
	}
	delThreshold, delLocktime, delAddrOff, delAddrCount, err := writeOwner(b, delegatorRewardsOwner)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(addPermissionlessValidatorSize)
	setEnvelope(ob, kindAddPermissionlessValidator, base, p)
	setValidator(ob, offAPVValidator, validator)
	setID(ob, offAPVChain, chain)
	if err := setSigner(ob, offAPVSigner, sig); err != nil {
		return nil, err
	}
	ob.SetList(offAPVStakeOuts, stakeListOff, stakeListCount)
	ob.SetList(offAPVStakeAddrs, stakeAddrOff, stakeAddrCount)
	setOwner(ob, offAPVValRewardsThreshold, offAPVValRewardsLocktime, offAPVValRewardsAddrs, valThreshold, valLocktime, valAddrOff, valAddrCount)
	setOwner(ob, offAPVDelRewardsThreshold, offAPVDelRewardsLocktime, offAPVDelRewardsAddrs, delThreshold, delLocktime, delAddrOff, delAddrCount)
	ob.SetUint32(offAPVDelegationShares, delegationShares)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &AddPermissionlessValidatorTx{spendingTx{msg}}, nil
}

// Validator describes the validator (offset read).
func (tx *AddPermissionlessValidatorTx) Validator() Validator {
	return readValidator(tx.root(), offAPVValidator)
}

// Chain is the id of the chain this validator is validating (offset read).
func (tx *AddPermissionlessValidatorTx) Chain() ids.ID { return readID(tx.root(), offAPVChain) }

// Signer is the BLS key for this validator (empty signer off the primary
// network) (offset read).
func (tx *AddPermissionlessValidatorTx) Signer() signer.Signer {
	return readSigner(tx.root(), offAPVSigner)
}

// StakeOuts is where staked tokens go when done validating (offset read).
func (tx *AddPermissionlessValidatorTx) StakeOuts() []*lux.TransferableOutput {
	return readExtraOuts(tx.root(), offAPVStakeOuts, offAPVStakeAddrs)
}

// ValidatorRewardsOwner is where validation rewards go when done validating.
func (tx *AddPermissionlessValidatorTx) ValidatorRewardsOwner() fx.Owner {
	return readOwner(tx.root(), offAPVValRewardsThreshold, offAPVValRewardsLocktime, offAPVValRewardsAddrs)
}

// DelegatorRewardsOwner is where delegation rewards go when done validating.
func (tx *AddPermissionlessValidatorTx) DelegatorRewardsOwner() fx.Owner {
	return readOwner(tx.root(), offAPVDelRewardsThreshold, offAPVDelRewardsLocktime, offAPVDelRewardsAddrs)
}

// DelegationShares is the fee (times 10,000) charged to delegators.
func (tx *AddPermissionlessValidatorTx) DelegationShares() uint32 {
	return tx.root().Uint32(offAPVDelegationShares)
}

// SyntacticVerify returns nil iff [tx] is valid.
func (tx *AddPermissionlessValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	v := tx.Validator()
	stakeOuts := tx.StakeOuts()
	switch {
	case v.NodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	case len(stakeOuts) == 0: // Ensure there is provided stake
		return errNoStake
	case tx.DelegationShares() > reward.PercentDenominator:
		return errTooManyShares
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	sig := tx.Signer()
	if err := verify.All(&v, sig, tx.ValidatorRewardsOwner(), tx.DelegatorRewardsOwner()); err != nil {
		return fmt.Errorf("failed to verify validator, signer, or rewards owners: %w", err)
	}

	hasKey := sig.Key() != nil
	isPrimaryNetwork := tx.Chain() == constants.PrimaryNetworkID
	if hasKey != isPrimaryNetwork {
		return fmt.Errorf(
			"%w: hasKey=%v != isPrimaryNetwork=%v",
			errInvalidSigner,
			hasKey,
			isPrimaryNetwork,
		)
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
		return fmt.Errorf("%w: weight %d != stake %d", errValidatorWeightMismatch, v.Wght, totalStakeWeight)
	}
	return nil
}

func (tx *AddPermissionlessValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddPermissionlessValidatorTx(tx)
}

// Initialize is a no-op; Runtime is passed explicitly to InitRuntime.
func (tx *AddPermissionlessValidatorTx) Initialize(ctx context.Context) error {
	return nil
}

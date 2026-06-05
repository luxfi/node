// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"

	safemath "github.com/luxfi/math"
)

var (
	ErrWeightTooSmall                  = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                  = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                   = errors.New("staking period is too short")
	ErrStakeTooLong                    = errors.New("staking period is too long")
	ErrFlowCheckFailed                 = errors.New("flow check failed")
	ErrNotValidator                    = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                   = errors.New("validator stake exceeds limit")
	ErrPeriodMismatch                  = errors.New("proposed staking period is not inside dependent staking period")
	ErrOverDelegated                   = errors.New("validator would be over delegated")
	ErrIsNotTransformChainTx           = errors.New("is not a transform net tx")
	ErrTimestampNotBeforeStartTime     = errors.New("chain timestamp not before start time")
	ErrAlreadyValidator                = errors.New("already a validator")
	ErrDuplicateValidator              = errors.New("duplicate validator")
	ErrDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	ErrWrongStakedAssetID              = errors.New("incorrect staked assetID")
	ErrLegacyUpgradeNotActive         = errors.New("legacy upgrade feature not active")
	ErrAddValidatorTxNotPermitted       = errors.New("AddValidatorTx is not permitted")
	ErrAddDelegatorTxNotPermitted       = errors.New("AddDelegatorTx is not permitted")
)

// verifyChainValidatorPrimaryNetworkRequirements verifies the primary
// network requirements for [chainValidator]. An error is returned if they
// are not fulfilled.
func verifyChainValidatorPrimaryNetworkRequirements(
	chainState state.Chain,
	chainValidator txs.Validator,
) error {
	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, chainValidator.NodeID)
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			chainValidator.NodeID,
			ErrNotValidator,
		)
	}
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			chainValidator.NodeID,
			err,
		)
	}

	// Ensure that the period this validator validates the specified chain
	// is a subset of the time they validate the primary network.
	if !txs.BoundedBy(
		chainState.GetTimestamp(),
		chainValidator.EndTime(),
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
	}

	return nil
}

// verifyAddValidatorTx permanently rejects AddValidatorTx; the legacy
// scheduled-staker flow has no role under activate-all-implicitly.
func verifyAddValidatorTx(
	*Backend,
	fee.Calculator,
	state.Chain,
	*txs.Tx,
	*txs.AddValidatorTx,
) (
	[]*lux.TransferableOutput,
	error,
) {
	return nil, ErrAddValidatorTxNotPermitted
}

// verifyAddChainValidatorTx carries out the validation for an
// AddChainValidatorTx.
func verifyAddChainValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddChainValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true); err != nil {
		return err
	}

	currentTimestamp := chainState.GetTimestamp()
	startTime := currentTimestamp
	duration := tx.EndTime().Sub(startTime)

	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	if err := verifyStakerStartTime(currentTimestamp, startTime); err != nil {
		return err
	}

	_, err := GetValidator(chainState, tx.ChainValidator.Chain, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"attempted to issue %w for %s on net %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.ChainValidator.Chain,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a net validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if err := verifyChainValidatorPrimaryNetworkRequirements(chainState, tx.Validator); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoAChainAuthorization(backend.Fx, chainState, sTx, tx.ChainValidator.Chain, tx.ChainAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// Returns the representation of [tx.NodeID] validating [tx.Chain].
// Returns true if [tx.NodeID] is a current validator of [tx.Chain].
// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [tx.NodeID] is a current/pending PoA validator of [tx.Chain].
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to remove a validator from [tx.Chain].
// * The flow checker passes.
func verifyRemoveChainValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.RemoveChainValidatorTx,
) (*state.Staker, bool, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Runtime); err != nil {
		return nil, false, err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true); err != nil {
		return nil, false, err
	}

	isCurrentValidator := true
	vdr, err := chainState.GetCurrentValidator(tx.Chain, tx.NodeID)
	if err == database.ErrNotFound {
		vdr, err = chainState.GetPendingValidator(tx.Chain, tx.NodeID)
		isCurrentValidator = false
	}
	if err != nil {
		// It isn't a current or pending validator.
		return nil, false, fmt.Errorf(
			"%s %w of %s: %w",
			tx.NodeID,
			ErrNotValidator,
			tx.Chain,
			err,
		)
	}

	if !vdr.Priority.IsPermissionedValidator() {
		return nil, false, ErrRemovePermissionlessValidator
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return vdr, isCurrentValidator, nil
	}

	baseTxCreds, err := verifyChainAuthorization(backend.Fx, chainState, sTx, tx.Chain, tx.ChainAuth)
	if err != nil {
		return nil, false, err
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return nil, false, err
	}
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return vdr, isCurrentValidator, nil
}

// verifyAddDelegatorTx permanently rejects AddDelegatorTx; the legacy
// scheduled-staker flow has no role under activate-all-implicitly.
func verifyAddDelegatorTx(
	*Backend,
	fee.Calculator,
	state.Chain,
	*txs.Tx,
	*txs.AddDelegatorTx,
) (
	[]*lux.TransferableOutput,
	error,
) {
	return nil, ErrAddDelegatorTxNotPermitted
}

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	currentTimestamp := chainState.GetTimestamp()
	startTime := currentTimestamp
	duration := tx.EndTime().Sub(startTime)

	if err := verifyStakerStartTime(currentTimestamp, startTime); err != nil {
		return err
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Chain)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case duration < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != validatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	_, err = GetValidator(chainState, tx.Chain, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.Chain,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			tx.Validator.NodeID,
			tx.Chain,
			err,
		)
	}

	if tx.Chain != constants.PrimaryNetworkID {
		if err := verifyChainValidatorPrimaryNetworkRequirements(chainState, tx.Validator); err != nil {
			return err
		}
	}

	outs := make([]*lux.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// verifyAddPermissionlessDelegatorTx carries out the validation for an
// AddPermissionlessDelegatorTx.
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessDelegatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		endTime          = tx.EndTime()
		startTime        = currentTimestamp
	)
	duration := endTime.Sub(startTime)

	if err := verifyStakerStartTime(currentTimestamp, startTime); err != nil {
		return err
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Chain)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return ErrWeightTooSmall

	case duration < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
		)
	}

	validator, err := GetValidator(chainState, tx.Chain, tx.Validator.NodeID)
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			tx.Validator.NodeID,
			tx.Chain,
			err,
		)
	}

	maximumWeight, err := safemath.Mul(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = math.MaxUint64
	}
	maximumWeight = min(maximumWeight, delegatorRules.maxValidatorStake)

	if !txs.BoundedBy(
		startTime,
		endTime,
		validator.StartTime,
		validator.EndTime,
	) {
		return ErrPeriodMismatch
	}
	overDelegated, err := overDelegated(
		chainState,
		validator,
		maximumWeight,
		tx.Validator.Wght,
		startTime,
		endTime,
	)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	outs := make([]*lux.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if tx.Chain != constants.PrimaryNetworkID {
		// Invariant: Delegators must only be able to reference validator
		//            transactions that implement [txs.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddChainValidatorTx. AddChainValidatorTx is the only
		//            permissioned validator, so we verify this delegator is
		//            pointing to a permissionless validator.
		if validator.Priority.IsPermissionedValidator() {
			return ErrDelegateToPermissionedValidator
		}
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to transfer ownership of [tx.Chain].
// * The flow checker passes.
func verifyTransferChainOwnershipTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.TransferChainOwnershipTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return nil
	}

	baseTxCreds, err := verifyChainAuthorization(backend.Fx, chainState, sTx, tx.Chain, tx.ChainAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// start time is not validated. Retained as the explicit call site so the
// staker-tx verifiers keep a single canonical timing-check seam.
func verifyStakerStartTime(_, _ time.Time) error {
	return nil
}

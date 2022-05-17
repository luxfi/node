// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"

	pChainValidator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ ProposalTx = &AddValidatorTx{}

	ErrWeightTooLarge            = errors.New("weight of this validator is too large")
	ErrStakeTooShort             = errors.New("staking period is too short")
	ErrStakeTooLong              = errors.New("staking period is too long")
	ErrFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
)

// Maximum future start time for staking/delegating
const MaxFutureStartTime = 24 * 7 * 2 * time.Hour

type AddValidatorTx struct {
	*unsigned.AddValidatorTx
}

// Attempts to verify this transaction with the provided state.
func (tx *AddValidatorTx) SemanticVerify(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) error {
	clock := verifier.Clock()
	startTime := tx.StartTime()
	maxLocalStartTime := clock.Time().Add(MaxFutureStartTime)
	if startTime.After(maxLocalStartTime) {
		return ErrFutureStakeTime
	}

	_, _, err := tx.Execute(verifier, parentState, creds)
	// We ignore [errFutureStakeTime] here because an advanceTimeTx will be
	// issued before this transaction is issued.
	if errors.Is(err, ErrFutureStakeTime) {
		return nil
	}
	return err
}

// Execute this transaction.
func (tx *AddValidatorTx) Execute(
	verifier TxVerifier,
	parentState state.Mutable,
	creds []verify.Verifiable,
) (
	state.Versioned,
	state.Versioned,
	error,
) {
	ctx := verifier.Ctx()

	// Verify the tx is well-formed
	if err := tx.SyntacticVerify(verifier.Ctx()); err != nil {
		return nil, nil, err
	}

	switch {
	case tx.Validator.Wght < verifier.PlatformConfig().MinValidatorStake: // Ensure validator is staking at least the minimum amount
		return nil, nil, pChainValidator.ErrWeightTooSmall
	case tx.Validator.Wght > verifier.PlatformConfig().MaxValidatorStake: // Ensure validator isn't staking too much
		return nil, nil, ErrWeightTooLarge
	case tx.Shares < verifier.PlatformConfig().MinDelegationFee:
		return nil, nil, ErrInsufficientDelegationFee
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < verifier.PlatformConfig().MinStakeDuration: // Ensure staking length is not too short
		return nil, nil, ErrStakeTooShort
	case duration > verifier.PlatformConfig().MaxStakeDuration: // Ensure staking length is not too long
		return nil, nil, ErrStakeTooLong
	}

	currentStakers := parentState.CurrentStakerChainState()
	pendingStakers := parentState.PendingStakerChainState()

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.Stake))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.Stake)

	if verifier.Bootstrapped() {
		currentTimestamp := parentState.GetTimestamp()
		// Ensure the proposed validator starts after the current time
		startTime := tx.StartTime()
		if !currentTimestamp.Before(startTime) {
			return nil, nil, fmt.Errorf(
				"validator's start time (%s) at or before current timestamp (%s)",
				startTime,
				currentTimestamp,
			)
		}

		// Ensure this validator isn't currently a validator.
		_, err := currentStakers.GetValidator(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is already a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Ensure this validator isn't about to become a validator.
		_, err = pendingStakers.GetValidatorTx(tx.Validator.NodeID)
		if err == nil {
			return nil, nil, fmt.Errorf(
				"%s is about to become a primary network validator",
				tx.Validator.NodeID,
			)
		}
		if err != database.ErrNotFound {
			return nil, nil, fmt.Errorf(
				"failed to find whether %s is about to become a validator: %w",
				tx.Validator.NodeID,
				err,
			)
		}

		// Verify the flowcheck
		if err := verifier.SemanticVerifySpend(
			parentState,
			tx,
			tx.Ins,
			outs,
			creds,
			verifier.PlatformConfig().AddStakerTxFee,
			ctx.AVAXAssetID,
		); err != nil {
			return nil, nil, fmt.Errorf("failed semanticVerifySpend: %w", err)
		}

		// Make sure the tx doesn't start too far in the future. This is done
		// last to allow SemanticVerification to explicitly check for this
		// error.
		maxStartTime := currentTimestamp.Add(MaxFutureStartTime)
		if startTime.After(maxStartTime) {
			return nil, nil, ErrFutureStakeTime
		}
	}

	// Set up the state if this tx is committed
	stx := &signed.Tx{
		Unsigned: tx.AddValidatorTx,
		Creds:    creds,
	}
	newlyPendingStakers := pendingStakers.AddStaker(stx)
	onCommitState := state.NewVersioned(parentState, currentStakers, newlyPendingStakers)

	// Consume the UTXOS
	utxos.ConsumeInputs(onCommitState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	utxos.ProduceOutputs(onCommitState, txID, ctx.AVAXAssetID, tx.Outs)

	// Set up the state if this tx is aborted
	onAbortState := state.NewVersioned(parentState, currentStakers, pendingStakers)
	// Consume the UTXOS
	utxos.ConsumeInputs(onAbortState, tx.Ins)
	// Produce the UTXOS
	utxos.ProduceOutputs(onAbortState, txID, ctx.AVAXAssetID, outs)

	return onCommitState, onAbortState, nil
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddValidatorTx) InitiallyPrefersCommit(verifier TxVerifier) bool {
	clock := verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
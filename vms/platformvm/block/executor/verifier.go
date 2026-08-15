// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/math"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/vm/chains/atomic"

	"github.com/luxfi/node/vms/components/gas"
	txfee "github.com/luxfi/node/vms/platformvm/txs/fee"
	validatorfee "github.com/luxfi/node/vms/platformvm/validators/fee"
)

var (
	_ block.Visitor = (*verifier)(nil)

	ErrConflictingBlockTxs         = errors.New("block contains conflicting transactions")
	ErrStandardBlockWithoutChanges = errors.New("StandardBlock performs no state changes")

	errIncorrectBlockHeight                  = errors.New("incorrect block height")
	errOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *txexecutor.Backend
	pChainHeight      uint64
}

func (v *verifier) AbortBlock(b *block.AbortBlock) error {
	if err := v.optionBlock(b); err != nil {
		return err
	}
	return v.abortBlock(b) // Must be the last validity check on the block
}

func (v *verifier) CommitBlock(b *block.CommitBlock) error {
	if err := v.optionBlock(b); err != nil {
		return err
	}
	return v.commitBlock(b) // Must be the last validity check on the block
}

func (v *verifier) ProposalBlock(b *block.ProposalBlock) error {
	if err := v.nonOptionBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onDecisionState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// Advance the time to [nextChainTime].
	nextChainTime := b.Timestamp()
	if _, err := txexecutor.AdvanceTimeTo(v.txExecutorBackend, onDecisionState, nextChainTime); err != nil {
		return err
	}

	feeCalculator := state.PickFeeCalculator(v.txExecutorBackend.Config, onDecisionState)
	// DecisionTxs, not Txs: the proposal tx is the LAST element of Txs(), and
	// processing it here charges a fee for a transaction the chain emitted about
	// itself. Neither calculator will price one — the static visitor and the
	// complexity visitor both refuse it — so asking costs the block its
	// verification, and it is asked again on every rebuild.
	//
	// The proposal tx has its own path immediately below, which executes it
	// without pricing it. That is the one that should see it.
	inputs, atomicRequests, onAcceptFunc, gasConsumed, _, err := v.processStandardTxs(
		b.DecisionTxs(),
		feeCalculator,
		onDecisionState,
		b.Parent(),
	)
	if err != nil {
		return err
	}

	onCommitState, err := state.NewDiffOn(onDecisionState)
	if err != nil {
		return err
	}

	onAbortState, err := state.NewDiffOn(onDecisionState)
	if err != nil {
		return err
	}

	return v.proposalBlock( // Must be the last validity check on the block
		b,
		b.Tx(),
		onDecisionState,
		gasConsumed,
		onCommitState,
		onAbortState,
		feeCalculator,
		inputs,
		atomicRequests,
		onAcceptFunc,
	)
}

func (v *verifier) StandardBlock(b *block.StandardBlock) error {
	if err := v.nonOptionBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// Advance the time to [b.Timestamp()].
	changed, err := txexecutor.AdvanceTimeTo(
		v.txExecutorBackend,
		onAcceptState,
		b.Timestamp(),
	)
	if err != nil {
		return err
	}

	feeCalculator := state.PickFeeCalculator(v.txExecutorBackend.Config, onAcceptState)
	return v.standardBlock( // Must be the last validity check on the block
		b,
		b.Txs(),
		feeCalculator,
		onAcceptState,
		changed,
	)
}

func (v *verifier) optionBlock(b block.TimestampedBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	// Option blocks must be uniquely generated from the parent ProposalBlock.
	// This means that the timestamp must be standardized to the parent's
	// timestamp.
	parentID := b.Parent()
	parentBlkTime := v.getTimestamp(parentID)
	blkTime := b.Timestamp()
	if !blkTime.Equal(parentBlkTime) {
		return fmt.Errorf(
			"%w parent block timestamp (%s) option block timestamp (%s)",
			errOptionBlockTimestampNotMatchingParent,
			parentBlkTime,
			blkTime,
		)
	}
	return nil
}

func (v *verifier) nonOptionBlock(b block.TimestampedBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	newChainTime := b.Timestamp()
	now := v.txExecutorBackend.Clk.Time()
	return txexecutor.VerifyNewChainTime(
		v.txExecutorBackend.Config.ValidatorFeeConfig,
		newChainTime,
		now,
		parentState,
	)
}

func (v *verifier) commonBlock(b block.Block) error {
	parentID := b.Parent()
	parent, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}

	expectedHeight := parent.Height() + 1
	height := b.Height()
	if expectedHeight != height {
		return fmt.Errorf(
			"%w expected %d, but found %d",
			errIncorrectBlockHeight,
			expectedHeight,
			height,
		)
	}
	return nil
}

// abortBlock populates the state of this block if [nil] is returned.
//
// Invariant: The call to abortBlock must be the last validity check on the
// block. If this function returns [nil], the block is cached as valid.
func (v *verifier) abortBlock(b block.Block) error {
	parentID := b.Parent()
	onAbortState, ok := v.getOnAbortState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.setBlockState(blkID, &blockState{
		statelessBlock:  b,
		onAcceptState:   onAbortState,
		timestamp:       onAbortState.GetTimestamp(),
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onAbortState,
			0,
		),
	})
	return nil
}

// commitBlock populates the state of this block if [nil] is returned.
//
// Invariant: The call to commitBlock must be the last validity check on the
// block. If this function returns [nil], the block is cached as valid.
func (v *verifier) commitBlock(b block.Block) error {
	parentID := b.Parent()
	onCommitState, ok := v.getOnCommitState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.setBlockState(blkID, &blockState{
		statelessBlock:  b,
		onAcceptState:   onCommitState,
		timestamp:       onCommitState.GetTimestamp(),
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onCommitState,
			0,
		),
	})
	return nil
}

// proposalBlock populates the state of this block if [nil] is returned.
//
// Invariant: The call to proposalBlock must be the last validity check on the
// block. If this function returns [nil], the block is cached as valid.
func (v *verifier) proposalBlock(
	b block.Block,
	tx *txs.Tx,
	onDecisionState state.Diff,
	gasConsumed gas.Gas,
	onCommitState state.Diff,
	onAbortState state.Diff,
	feeCalculator txfee.Calculator,
	inputs set.Set[ids.ID],
	atomicRequests map[ids.ID]*atomic.Requests,
	onAcceptFunc func(),
) error {
	err := txexecutor.ProposalTx(
		v.txExecutorBackend,
		feeCalculator,
		tx,
		onCommitState,
		onAbortState,
	)
	if err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return err
	}

	onCommitState.AddTx(tx, status.Committed)
	onAbortState.AddTx(tx, status.Aborted)

	v.Mempool.Remove(tx)

	blkID := b.ID()
	v.setBlockState(blkID, &blockState{
		proposalBlockState: proposalBlockState{
			onDecisionState: onDecisionState,
			onCommitState:   onCommitState,
			onAbortState:    onAbortState,
		},

		statelessBlock: b,

		onAcceptFunc: onAcceptFunc,

		inputs: inputs,
		// It is safe to use [b.onAbortState] here because the timestamp will
		// never be modified by a legacy abort block and the timestamp will
		timestamp:       onAbortState.GetTimestamp(),
		atomicRequests:  atomicRequests,
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onCommitState,
			gasConsumed,
		),
	})
	return nil
}

// standardBlock populates the state of this block if [nil] is returned.
//
// Invariant: The call to standardBlock must be the last validity check on the
// block. If this function returns [nil], the block is cached as valid.
func (v *verifier) standardBlock(
	b block.Block,
	txs []*txs.Tx,
	feeCalculator txfee.Calculator,
	onAcceptState state.Diff,
	changedDuringAdvanceTime bool,
) error {
	inputs, atomicRequests, onAcceptFunc, gasConsumed, lowBalanceL1ValidatorsEvicted, err := v.processStandardTxs(
		txs,
		feeCalculator,
		onAcceptState,
		b.Parent(),
	)
	if err != nil {
		return err
	}

	// Verify that the block performs changes. If it does not, it never should
	// have been issued.
	if hasChanges := changedDuringAdvanceTime || len(txs) > 0 || lowBalanceL1ValidatorsEvicted; !hasChanges {
		return ErrStandardBlockWithoutChanges
	}

	v.Mempool.Remove(txs...)

	blkID := b.ID()
	v.setBlockState(blkID, &blockState{
		statelessBlock: b,

		onAcceptState: onAcceptState,
		onAcceptFunc:  onAcceptFunc,

		timestamp:       onAcceptState.GetTimestamp(),
		inputs:          inputs,
		atomicRequests:  atomicRequests,
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onAcceptState,
			gasConsumed,
		),
	})
	return nil
}

func (v *verifier) processStandardTxs(txs []*txs.Tx, feeCalculator txfee.Calculator, diff state.Diff, parentID ids.ID) (
	set.Set[ids.ID],
	map[ids.ID]*atomic.Requests,
	func(),
	gas.Gas,
	bool,
	error,
) {
	// Complexity is limited first to avoid processing too large of a block.
	var gasConsumed gas.Gas
	{
		var blockComplexity gas.Dimensions
		for _, tx := range txs {
			txComplexity, err := txfee.TxComplexity(tx.Unsigned)
			if err != nil {
				txID := tx.ID()
				v.MarkDropped(txID, err)
				return nil, nil, nil, 0, false, err
			}

			blockComplexity, err = blockComplexity.Add(&txComplexity)
			if err != nil {
				return nil, nil, nil, 0, false, fmt.Errorf("block complexity overflow: %w", err)
			}
		}

		var err error
		gasConsumed, err = blockComplexity.ToGas(v.txExecutorBackend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, nil, nil, 0, false, fmt.Errorf("block gas overflow: %w", err)
		}

		// If this block exceeds the available capacity, ConsumeGas will return
		// an error.
		feeState := diff.GetFeeState()
		feeState, err = feeState.ConsumeGas(gasConsumed)
		if err != nil {
			return nil, nil, nil, 0, false, err
		}

		// Updating the fee state prior to executing the transactions is fine
		// because the fee calculator was already created.
		diff.SetFeeState(feeState)
	}

	var (
		onAcceptFunc   func()
		inputs         set.Set[ids.ID]
		funcs          = make([]func(), 0, len(txs))
		atomicRequests = make(map[ids.ID]*atomic.Requests)
	)
	for _, tx := range txs {
		txInputs, txAtomicRequests, onAccept, err := txexecutor.StandardTx(
			v.txExecutorBackend,
			feeCalculator,
			tx,
			diff,
		)
		if err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err) // cache tx as dropped
			return nil, nil, nil, 0, false, err
		}
		// ensure it doesn't overlap with current input batch
		if inputs.Overlaps(txInputs) {
			return nil, nil, nil, 0, false, ErrConflictingBlockTxs
		}
		// Add UTXOs to batch
		inputs = inputs.Union(txInputs)

		diff.AddTx(tx, status.Committed)
		if onAccept != nil {
			funcs = append(funcs, onAccept)
		}

		for chainID, txRequests := range txAtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := atomicRequests[chainID]
			if !exists {
				atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if err := v.verifyUniqueInputs(parentID, inputs); err != nil {
		return nil, nil, nil, 0, false, err
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	// After processing all the transactions, deactivate any L1 validators that
	// might not have sufficient fee to pay for the next second.
	//
	// This ensures that L1 validators are not undercharged for the next second.
	lowBalanceL1ValidatorsEvicted, err := deactivateLowBalanceL1Validators(
		v.txExecutorBackend.Config.ValidatorFeeConfig,
		diff,
	)
	if err != nil {
		return nil, nil, nil, 0, false, fmt.Errorf("failed to deactivate low balance L1 validators: %w", err)
	}

	return inputs, atomicRequests, onAcceptFunc, gasConsumed, lowBalanceL1ValidatorsEvicted, nil
}

func calculateBlockMetrics(
	config *config.Internal,
	blk block.Block,
	s state.Chain,
	gasConsumed gas.Gas,
) metrics.Block {
	var (
		gasState        = s.GetFeeState()
		validatorExcess = s.GetL1ValidatorExcess()
	)
	return metrics.Block{
		Block: blk,

		GasConsumed: gasConsumed,
		GasState:    gasState,
		GasPrice: gas.CalculatePrice(
			config.DynamicFeeConfig.MinPrice,
			gasState.Excess,
			config.DynamicFeeConfig.ExcessConversionConstant,
		),

		ActiveL1Validators: s.NumActiveL1Validators(),
		ValidatorExcess:    validatorExcess,
		ValidatorPrice: gas.CalculatePrice(
			config.ValidatorFeeConfig.MinPrice,
			validatorExcess,
			config.ValidatorFeeConfig.ExcessConversionConstant,
		),
		AccruedValidatorFees: s.GetAccruedFees(),
	}
}

// deactivateLowBalanceL1Validators deactivates any L1 validators that might not
// have sufficient fees to pay for the next second. The returned bool will be
// true if at least one L1 validator was deactivated.
func deactivateLowBalanceL1Validators(
	config validatorfee.Config,
	diff state.Diff,
) (bool, error) {
	var (
		accruedFees       = diff.GetAccruedFees()
		validatorFeeState = validatorfee.State{
			Current: gas.Gas(diff.NumActiveL1Validators()),
			Excess:  diff.GetL1ValidatorExcess(),
		}
		potentialCost = validatorFeeState.CostOf(
			config,
			1, // 1 second
		)
	)
	potentialAccruedFees, err := math.Add(accruedFees, potentialCost)
	if err != nil {
		return false, fmt.Errorf("could not calculate potentially accrued fees: %w", err)
	}

	// Invariant: Proposal transactions do not impact L1 validator state.
	l1ValidatorIterator, err := diff.GetActiveL1ValidatorsIterator()
	if err != nil {
		return false, fmt.Errorf("could not iterate over active L1 validators: %w", err)
	}

	var l1ValidatorsToDeactivate []state.L1Validator
	for l1ValidatorIterator.Next() {
		l1Validator := l1ValidatorIterator.Value()
		// If the validator has exactly the right amount of fee for the next
		// second we should not remove them here.
		//
		// GetActiveL1ValidatorsIterator iterates in order of increasing
		// EndAccumulatedFee, so we can break early.
		if l1Validator.EndAccumulatedFee >= potentialAccruedFees {
			break
		}

		l1ValidatorsToDeactivate = append(l1ValidatorsToDeactivate, l1Validator)
	}

	// The iterator must be released prior to attempting to write to the
	// diff.
	l1ValidatorIterator.Release()

	for _, l1Validator := range l1ValidatorsToDeactivate {
		l1Validator.EndAccumulatedFee = 0
		if err := diff.PutL1Validator(l1Validator); err != nil {
			return false, fmt.Errorf("could not deactivate L1 validator %s: %w", l1Validator.ValidationID, err)
		}
	}
	return len(l1ValidatorsToDeactivate) > 0, nil
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/stakingparams"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/validators/uptime"
)

var (
	_ block.Visitor = (*options)(nil)

	ErrNotOracle                       = errors.New("block doesn't have options")
	errUnexpectedProposalTxType        = errors.New("unexpected proposal transaction type")
	errFailedFetchingStakerTx          = errors.New("failed fetching staker transaction")
	errUnexpectedStakerTxType          = errors.New("unexpected staker transaction type")
	errFailedFetchingPrimaryStaker     = errors.New("failed fetching primary staker")
	errFailedFetchingNetTransformation = errors.New("failed fetching net transformation")
	errFailedCalculatingUptime         = errors.New("failed calculating uptime")
)

// options supports build new option blocks
type options struct {
	// inputs populated before calling this struct's methods:
	log                     log.Logger
	primaryUptimePercentage float64
	// stakingPolicyAt resolves the governed primary-network policy binding a
	// staker that bonded at a given unix time. Nil falls back to
	// primaryUptimePercentage, so an ungoverned node is byte-for-byte
	// unchanged.
	stakingPolicyAt func(int64) stakingparams.Params
	uptimes         uptime.Calculator
	state           state.Chain

	// outputs populated by this struct's methods:
	preferredBlock block.Block
	alternateBlock block.Block
}

func (*options) AbortBlock(*block.AbortBlock) error {
	return ErrNotOracle
}

func (*options) CommitBlock(*block.CommitBlock) error {
	return ErrNotOracle
}

func (o *options) ProposalBlock(b *block.ProposalBlock) error {
	timestamp := b.Timestamp()
	blkID := b.ID()
	nextHeight := b.Height() + 1

	commitBlock, err := block.NewCommitBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}

	abortBlock, err := block.NewAbortBlock(timestamp, blkID, nextHeight)
	if err != nil {
		return fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	prefersCommit, err := o.prefersCommit(b.Tx())
	if err != nil {
		o.log.Debug("falling back to prefer commit",
			"error", err,
		)
		// We fall back to commit here to err on the side of over-rewarding
		// rather than under-rewarding.
		//
		// Invariant: We must not return the error here, because the error would
		// be treated as fatal. Errors can occur here due to a malicious block
		// proposer or even in unusual virtuous cases.
		prefersCommit = true
	}

	if prefersCommit {
		o.preferredBlock = commitBlock
		o.alternateBlock = abortBlock
	} else {
		o.preferredBlock = abortBlock
		o.alternateBlock = commitBlock
	}
	return nil
}

func (*options) StandardBlock(*block.StandardBlock) error {
	return ErrNotOracle
}

func (o *options) prefersCommit(tx *txs.Tx) (bool, error) {
	unsignedTx, ok := tx.Unsigned.(*txs.RewardValidatorTx)
	if !ok {
		return false, fmt.Errorf("%w: %T", errUnexpectedProposalTxType, tx.Unsigned)
	}

	stakerTx, _, err := o.state.GetTx(unsignedTx.TxID())
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedFetchingStakerTx, err)
	}

	staker, ok := stakerTx.Unsigned.(txs.Staker)
	if !ok {
		return false, fmt.Errorf("%w: %T", errUnexpectedStakerTxType, stakerTx.Unsigned)
	}

	nodeID := staker.NodeID()
	primaryNetworkValidator, err := o.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		nodeID,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedFetchingPrimaryStaker, err)
	}

	netID := staker.ChainID()
	// Judge the validator on the uptime rule that was in force when it BONDED,
	// not the one in force now. Passing StartTime rather than the current chain
	// time is what stops a governed uptime requirement from being retroactive —
	// otherwise a stake majority could raise the bar the day before a rival's
	// stake matures and take its reward. With no governed history this is the
	// compiled-in --uptime-requirement, unchanged.
	expectedUptimePercentage := o.primaryUptimePercentage
	if o.stakingPolicyAt != nil {
		expectedUptimePercentage = float64(
			o.stakingPolicyAt(primaryNetworkValidator.StartTime.Unix()).UptimeRequirement,
		) / reward.PercentDenominator
	}
	if netID != constants.PrimaryNetworkID {
		transformNet, err := txexecutor.GetTransformChainTx(o.state, netID)
		if err != nil {
			return false, fmt.Errorf("%w: %w", errFailedFetchingNetTransformation, err)
		}

		expectedUptimePercentage = float64(transformNet.UptimeRequirement()) / reward.PercentDenominator
	}

	uptime, err := o.uptimes.CalculateUptimePercentFrom(
		nodeID,
		netID,
		primaryNetworkValidator.StartTime,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFailedCalculatingUptime, err)
	}

	return uptime >= expectedUptimePercentage, nil
}

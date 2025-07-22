// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/validators"
	"github.com/luxfi/node/vms/txs/mempool"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced              = errors.New("chain not synced")
	ErrImportTxWhilePartialSyncing = errors.New("issuing an import tx is not allowed while partial syncing")
)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (linear.Block, error)
	GetStatelessBlock(blkID ids.ID) (block.Block, error)
	NewBlock(block.Block) linear.Block

	// VerifyTx verifies that the transaction can be issued based on the currently
	// preferred state. This should *not* be used to verify transactions in a block.
	VerifyTx(tx *txs.Tx) error

	// VerifyUniqueInputs verifies that the inputs are not duplicated in the
	// provided blk or any of its ancestors pinned in memory.
	VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error
}

func NewManager(
	mempool mempool.Mempool[*txs.Tx],
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend *executor.Backend,
	validatorManager validators.Manager,
) Manager {
	lastAccepted := s.GetLastAccepted()
	backend := &backend{
		Mempool:      mempool,
		lastAccepted: lastAccepted,
		state:        s,
		ctx:          txExecutorBackend.Ctx,
		blkIDToState: map[ids.ID]*blockState{},
	}

	return &manager{
		backend: backend,
		acceptor: &acceptor{
			backend:    backend,
			metrics:    metrics,
			validators: validatorManager,
		},
		rejector: &rejector{
			backend:         backend,
			addTxsToMempool: !txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		},
		preferred:         lastAccepted,
		txExecutorBackend: txExecutorBackend,
	}
}

type manager struct {
	*backend
	acceptor block.Visitor
	rejector block.Visitor

	preferred         ids.ID
	txExecutorBackend *executor.Backend
}

func (m *manager) GetBlock(blkID ids.ID) (linear.Block, error) {
	blk, err := m.backend.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (block.Block, error) {
	return m.backend.GetBlock(blkID)
}

func (m *manager) NewBlock(blk block.Block) linear.Block {
	return &Block{
		manager: m,
		Block:   blk,
	}
}

func (m *manager) SetPreference(blkID ids.ID) {
	m.preferred = blkID
}

func (m *manager) Preferred() ids.ID {
	return m.preferred
}

func (m *manager) VerifyTx(tx *txs.Tx) error {
	if !m.txExecutorBackend.Bootstrapped.Get() {
		return ErrChainNotSynced
	}

	// If partial sync is enabled, this node isn't guaranteed to have the full
	// UTXO set from shared memory. To avoid issuing invalid transactions,
	// issuance of an ImportTx during this state is completely disallowed.
	if m.txExecutorBackend.Config.PartialSyncPrimaryNetwork {
		if _, isImportTx := tx.Unsigned.(*txs.ImportTx); isImportTx {
			return ErrImportTxWhilePartialSyncing
		}
	}

	recommendedPChainHeight, err := m.ctx.ValidatorState.GetMinimumHeight(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to fetch P-chain height: %w", err)
	}
	err = executor.VerifyWarpMessages(
		context.TODO(),
		m.ctx.NetworkID,
		m.ctx.ValidatorState,
		recommendedPChainHeight,
		tx.Unsigned,
	)
	if err != nil {
		return fmt.Errorf("failed verifying warp messages: %w", err)
	}

	stateDiff, err := state.NewDiff(m.preferred, m)
	if err != nil {
		return fmt.Errorf("failed creating state diff: %w", err)
	}

	nextBlkTime, _, err := state.NextBlockTime(
		m.txExecutorBackend.Config.ValidatorFeeConfig,
		stateDiff,
		m.txExecutorBackend.Clk,
	)
	if err != nil {
		return fmt.Errorf("failed selecting next block time: %w", err)
	}

	_, err = executor.AdvanceTimeTo(m.txExecutorBackend, stateDiff, nextBlkTime)
	if err != nil {
		return fmt.Errorf("failed to advance the chain time: %w", err)
	}

	if timestamp := stateDiff.GetTimestamp(); m.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp) {
		complexity, err := fee.TxComplexity(tx.Unsigned)
		if err != nil {
			return fmt.Errorf("failed to calculate tx complexity: %w", err)
		}
		gas, err := complexity.ToGas(m.txExecutorBackend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return fmt.Errorf("failed to calculate tx gas: %w", err)
		}

		// TODO: After the mempool is updated, convert this check to use the
		// maximum mempool capacity.
		feeState := stateDiff.GetFeeState()
		if gas > feeState.Capacity {
			return fmt.Errorf("tx exceeds current gas capacity: %d > %d", gas, feeState.Capacity)
		}
	}

	feeCalculator := state.PickFeeCalculator(m.txExecutorBackend.Config, stateDiff)
	_, _, _, err = executor.StandardTx(
		m.txExecutorBackend,
		feeCalculator,
		tx,
		stateDiff,
	)
	if err != nil {
		return fmt.Errorf("failed execution: %w", err)
	}
	return nil
}

func (m *manager) VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error {
	return m.backend.verifyUniqueInputs(blkID, inputs)
}

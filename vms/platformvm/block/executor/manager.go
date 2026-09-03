// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	platformblock "github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/validators"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/vm/chain"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced = errors.New("chain not synced")
)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted chain.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (chain.Block, error)
	GetStatelessBlock(blkID ids.ID) (platformblock.Block, error)
	NewBlock(platformblock.Block) chain.Block

	// VerifyTx verifies that the transaction can be issued based on the currently
	// preferred state. This should *not* be used to verify transactions in a chain.
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
	// stateLock serializes a block's accept/reject state commit with the VM's
	// OTHER writers of the same shared state — the peer-lifecycle (Disconnect)
	// and normal-ops (Start/StopTracking) uptime flushes, which run on
	// goroutines the consensus engine does not serialize. The platform VM owns
	// it and holds the SAME lock in those paths; without it a peer disconnect's
	// state.Commit races a block accept's state.CommitBatch inside state.write()
	// (concurrent Go map writes → fatal). Must be non-nil (the VM always
	// supplies &vm.stateLock).
	stateLock *sync.Mutex,
) Manager {
	lastAccepted := s.GetLastAccepted()
	backend := &backend{
		Mempool:      mempool,
		lastAccepted: lastAccepted,
		state:        s,
		rt:           txExecutorBackend.Runtime,
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
			backend: backend,
		},
		preferred:         lastAccepted,
		txExecutorBackend: txExecutorBackend,
		validatorManager:  validatorManager,
		stateLock:         stateLock,
		Log:               log.Noop(),
	}
}

type manager struct {
	*backend
	acceptor platformblock.Visitor
	rejector platformblock.Visitor

	preferred         ids.ID
	txExecutorBackend *executor.Backend
	validatorManager  validators.Manager
	// stateLock serializes block accept/reject (Block.Accept/Reject) with the
	// VM's peer-lifecycle / normal-ops state commits. See NewManager.
	stateLock *sync.Mutex
	Log       log.Logger
}

func (m *manager) GetBlock(blkID ids.ID) (chain.Block, error) {
	blk, err := m.backend.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (platformblock.Block, error) {
	return m.backend.GetBlock(blkID)
}

func (m *manager) NewBlock(blk platformblock.Block) chain.Block {
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

	// Get current height from validator manager
	recommendedPChainHeight, err := m.validatorManager.GetCurrentHeight(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to fetch P-chain height: %w", err)
	}
	err = executor.VerifyWarpMessages(
		context.TODO(),
		m.rt.NetworkID,
		m.validatorManager,
		recommendedPChainHeight,
		tx.Unsigned,
	)
	if err != nil {
		return fmt.Errorf("failed verifying warp messages: %w", err)
	}

	// Use preferred block for state diff. If preferred state isn't available
	// (race between block acceptance and preference update), fall back to
	// the last accepted block which always has committed state.
	preferredID := m.preferred
	stateDiff, err := state.NewDiff(preferredID, m)
	if err != nil {
		lastAccepted := m.state.GetLastAccepted()
		if lastAccepted != preferredID {
			stateDiff, err = state.NewDiff(lastAccepted, m)
		}
		if err != nil {
			return fmt.Errorf("failed creating state diff: %w", err)
		}
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

	{
		complexity, err := fee.TxComplexity(tx.Unsigned)
		if err != nil {
			return fmt.Errorf("failed to calculate tx complexity: %w", err)
		}
		gas, err := complexity.ToGas(m.txExecutorBackend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return fmt.Errorf("failed to calculate tx gas: %w", err)
		}

		// Check against current fee state capacity. This is intentionally
		// checked against the chain tip rather than mempool capacity.
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

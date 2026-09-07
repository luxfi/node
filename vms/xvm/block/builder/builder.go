// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/timer/mockable"
	vmcore "github.com/luxfi/vm"
	chain "github.com/luxfi/vm/chain"

	blockexecutor "github.com/luxfi/node/vms/xvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
)

// targetBlockSize is the max block size we aim to produce
const targetBlockSize = 128 * constants.KiB

var (
	_ Builder = (*builder)(nil)

	ErrNoTransactions = errors.New("no transactions")
)

type Builder interface {
	// WaitForEvent waits until there is at least one tx available to the
	// builder.
	WaitForEvent(ctx context.Context) (vmcore.Message, error)
	// BuildBlock can be called to attempt to create a new block
	BuildBlock(context.Context) (chain.Block, error)
}

// builder implements a simple builder to convert txs into valid blocks
type builder struct {
	backend *txexecutor.Backend
	manager blockexecutor.Manager
	clk     *mockable.Clock

	// Pool of all txs that may be able to be added
	mempool mempool.Mempool[*txs.Tx]
}

func New(
	backend *txexecutor.Backend,
	manager blockexecutor.Manager,
	clk *mockable.Clock,
	mempool mempool.Mempool[*txs.Tx],
) Builder {
	return &builder{
		backend: backend,
		manager: manager,
		clk:     clk,
		mempool: mempool,
	}
}

func (b *builder) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	return b.mempool.WaitForEvent(ctx)
}

// BuildBlock builds a block to be added to consensus.
func (b *builder) BuildBlock(context.Context) (chain.Block, error) {
	if !b.backend.Log.IsZero() {
		b.backend.Log.Debug("starting to attempt to build a block")
	}

	// Get the block to build on top of and retrieve the new block's context.
	preferredID := b.manager.Preferred()
	preferred, err := b.manager.GetStatelessBlock(preferredID)
	if err != nil {
		return nil, err
	}

	preferredHeight := preferred.Height()
	preferredTimestamp := preferred.Timestamp()

	nextHeight := preferredHeight + 1
	nextTimestamp := b.clk.Time() // [timestamp] = max(now, parentTime)
	if preferredTimestamp.After(nextTimestamp) {
		nextTimestamp = preferredTimestamp
	}

	stateDiff, err := state.NewDiff(preferredID, b.manager)
	if err != nil {
		return nil, err
	}

	var (
		blockTxs      []*txs.Tx
		inputs        = set.NewSet[ids.ID]()
		remainingSize = targetBlockSize
	)
	for {
		tx, exists := b.mempool.Peek()
		// Invariant: [mempool.MaxTxSize] < [targetBlockSize]. This guarantees
		// that we will only stop building a block once there are no
		// transactions in the mempool or the block is at least
		// [targetBlockSize - mempool.MaxTxSize] bytes full.
		if !exists || len(tx.Bytes()) > remainingSize {
			break
		}
		b.mempool.Remove(tx)

		// Invariant: [tx] has already been syntactically verified.

		txDiff, err := state.NewDiffOn(stateDiff)
		if err != nil {
			return nil, err
		}

		err = tx.Unsigned.Visit(&txexecutor.SemanticVerifier{
			Backend: b.backend,
			State:   txDiff,
			Tx:      tx,
		})
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}

		executor := &txexecutor.Executor{
			State:  txDiff,
			Tx:     tx,
			Inputs: set.NewSet[ids.ID](0), // Initialize empty set for imported inputs
		}
		err = tx.Unsigned.Visit(executor)
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}

		if inputs.Overlaps(executor.Inputs) {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, blockexecutor.ErrConflictingBlockTxs)
			continue
		}
		err = b.manager.VerifyUniqueInputs(preferredID, executor.Inputs)
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}
		inputs = inputs.Union(executor.Inputs)

		txDiff.AddTx(tx)
		txDiff.Apply(stateDiff)

		remainingSize -= len(tx.Bytes())
		blockTxs = append(blockTxs, tx)
	}

	if len(blockTxs) == 0 {
		return nil, ErrNoTransactions
	}

	// Stamp the xvm execution_root computed over the post-block state into the
	// block so the verifier can recompute and check it. The root is the tagged
	// Merkle-tree fold over the post-block state (utxo ‖ asset ‖ tx ‖ height);
	// the builder and the executor share exactly one computation
	// (BlockExecutionRoot) so the stamped root and the verified root can never
	// disagree.
	parentRoot := preferred.MerkleRoot()
	root, err := blockexecutor.BlockExecutionRoot(
		parentRoot,
		blockTxs,
		stateDiff,
		nextHeight,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute block execution root: %w", err)
	}
	statelessBlk, err := block.NewStandardBlockWithRoot(
		preferredID,
		nextHeight,
		nextTimestamp,
		root,
		blockTxs,
	)
	if err != nil {
		return nil, err
	}

	return b.manager.NewBlock(statelessBlk), nil
}

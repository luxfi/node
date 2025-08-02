// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core"
	"github.com/luxfi/node/quasar/chain"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"

	blockexecutor "github.com/luxfi/node/vms/xvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
)

// targetBlockSize is the max block size we aim to produce
const targetBlockSize = 128 * units.KiB

var (
	_ Builder = (*builder)(nil)

	ErrNoTransactions = errors.New("no transactions")
)

type Builder interface {
	// WaitForEvent waits until there is at least one tx available to the
	// builder.
	WaitForEvent(ctx context.Context) (core.Message, error)
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

func (b *builder) WaitForEvent(ctx context.Context) (core.Message, error) {
	return b.mempool.WaitForEvent(ctx)
}

// BuildBlock builds a block to be added to consensus.
func (b *builder) BuildBlock(context.Context) (chain.Block, error) {
	ctx := b.backend.Ctx
	ctx.Log.Debug("starting to attempt to build a block")

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
		inputs        set.Set[ids.ID]
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
			Codec: b.backend.Codec,
			State: txDiff,
			Tx:    tx,
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
		err = b.manager.VerifyUniqueInputs(preferredID, inputs)
		if err != nil {
			txID := tx.ID()
			b.mempool.MarkDropped(txID, err)
			continue
		}
		inputs.Union(executor.Inputs)

		txDiff.AddTx(tx)
		txDiff.Apply(stateDiff)

		remainingSize -= len(tx.Bytes())
		blockTxs = append(blockTxs, tx)
	}

	if len(blockTxs) == 0 {
		return nil, ErrNoTransactions
	}

	statelessBlk, err := block.NewStandardBlock(
		preferredID,
		nextHeight,
		nextTimestamp,
		blockTxs,
		b.backend.Codec,
	)
	if err != nil {
		return nil, err
	}

	return b.manager.NewBlock(statelessBlk), nil
}

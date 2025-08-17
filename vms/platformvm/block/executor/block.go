// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"time"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/database"
	"github.com/luxfi/node/vms/platformvm/block"
)

var (
	_ chain.Block = (*Block)(nil)
	// OracleBlock is not available in consensus package
	// _ chain.OracleBlock = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	block.Block
	manager *manager
}

func (b *Block) Verify(context.Context) error {
	blkID := b.ID()
	if _, ok := b.manager.blkIDToState[blkID]; ok {
		// This block has already been verified.
		return nil
	}

	return b.Visit(b.manager.verifier)
}

func (b *Block) Accept(context.Context) error {
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject(context.Context) error {
	return b.Visit(b.manager.rejector)
}

func (b *Block) Status() choices.Status {
	blkID := b.ID()
	// If this block is an accepted Proposal block with no accepted children, it
	// will be in [blkIDToState], but we should return accepted, not processing,
	// so we do this check.
	if b.manager.lastAccepted == blkID {
		return choices.Accepted
	}
	// Check if the block is in memory. If so, it's processing.
	if _, ok := b.manager.blkIDToState[blkID]; ok {
		return choices.Processing
	}
	// Block isn't in memory. Check in the database.
	_, err := b.manager.state.GetStatelessBlock(blkID)
	switch err {
	case nil:
		return choices.Accepted

	case database.ErrNotFound:
		// choices.Unknown means we don't have the bytes of the block.
		// In this case, we do, so we return choices.Processing.
		return choices.Processing

	default:
		b.manager.Log.Error(
			"dropping unhandled database error",
			"error", err,
		)
		return choices.Processing
	}
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.ID())
}

func (b *Block) Options(context.Context) ([2]chain.Block, error) {
	options := options{
		log:                     b.manager.Log,
		primaryUptimePercentage: b.manager.txExecutorBackend.Config.UptimePercentage,
		uptimes:                 b.manager.txExecutorBackend.Uptimes,
		state:                   b.manager.backend.state,
	}
	if err := b.Block.Visit(&options); err != nil {
		return [2]chain.Block{}, err
	}

	return [2]chain.Block{
		b.manager.NewBlock(options.preferredBlock),
		b.manager.NewBlock(options.alternateBlock),
	}, nil
}

// FPCVotes implements the chain.Block interface
// Returns embedded fast-path consensus vote references
func (b *Block) FPCVotes() [][]byte {
	return nil
}

// EpochBit implements the chain.Block interface
// Returns the epoch fence bit for FPC
func (b *Block) EpochBit() bool {
	return false
}

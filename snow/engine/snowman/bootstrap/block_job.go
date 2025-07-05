// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common/queue"
	"github.com/luxfi/node/snow/engine/snowman/block"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
)

var errMissingDependenciesOnAccept = errors.New("attempting to accept a block with missing dependencies")

type parser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vm                      block.ChainVM
}

func (p *parser) Parse(ctx context.Context, blkBytes []byte) (queue.Job, error) {
	blk, err := p.vm.ParseBlock(ctx, blkBytes)
	if err != nil {
		return nil, err
	}
	return &blockJob{
		log:         p.log,
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		blk:         blk,
		vm:          p.vm,
	}, nil
}

type blockJob struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	blk                     snowman.Block
	vm                      block.ChainVM
}

func (b *blockJob) ID() ids.ID {
	return b.blk.ID()
}

// isAccepted checks if a block has been accepted by comparing its ID with the
// accepted block at its height
func (b *blockJob) isAccepted(ctx context.Context, blkID ids.ID, height uint64) (bool, error) {
	acceptedID, err := b.vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		// If we can't get the block at this height, we can't determine if it's accepted
		return false, nil
	}
	return acceptedID == blkID, nil
}

func (b *blockJob) MissingDependencies(ctx context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
	parentID := b.blk.Parent()
	
	parent, err := b.vm.GetBlock(ctx, parentID)
	if err != nil {
		missing.Add(parentID)
		return missing, nil
	}
	
	// Check if parent is accepted
	isAccepted, err := b.isAccepted(ctx, parentID, parent.Height())
	if err != nil || !isAccepted {
		missing.Add(parentID)
	}
	return missing, nil
}

func (b *blockJob) HasMissingDependencies(ctx context.Context) (bool, error) {
	parentID := b.blk.Parent()
	
	parent, err := b.vm.GetBlock(ctx, parentID)
	if err != nil {
		return true, nil
	}
	
	// Check if parent is accepted
	isAccepted, err := b.isAccepted(ctx, parentID, parent.Height())
	if err != nil || !isAccepted {
		return true, nil
	}
	return false, nil
}

func (b *blockJob) Execute(ctx context.Context) error {
	hasMissingDeps, err := b.HasMissingDependencies(ctx)
	if err != nil {
		return err
	}
	if hasMissingDeps {
		b.numDropped.Inc()
		return errMissingDependenciesOnAccept
	}
	
	blkID := b.blk.ID()
	blkHeight := b.blk.Height()
	
	// Check if this block has already been accepted
	isAccepted, err := b.isAccepted(ctx, blkID, blkHeight)
	if err != nil {
		return fmt.Errorf("failed to check if block is accepted: %w", err)
	}
	
	if isAccepted {
		// Block is already accepted, nothing to do
		return nil
	}
	
	// Check if the last accepted block height is greater than or equal to this block's height
	lastAcceptedID, err := b.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	
	lastAcceptedBlk, err := b.vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	
	if lastAcceptedBlk.Height() >= blkHeight {
		// There's already a block accepted at this height or higher, and it's not this block
		// This means this block has been rejected
		b.numDropped.Inc()
		return fmt.Errorf("attempting to execute block at height %d when last accepted height is %d", blkHeight, lastAcceptedBlk.Height())
	}
	
	// The block hasn't been decided yet, so we should verify and accept it
	if err := b.blk.Verify(ctx); err != nil {
		b.log.Error("block failed verification during bootstrapping",
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
		return fmt.Errorf("failed to verify block in bootstrapping: %w", err)
	}

	b.numAccepted.Inc()
	b.log.Trace("accepting block in bootstrapping",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", b.blk.Height()),
		zap.Time("timestamp", b.blk.Timestamp()),
	)
	if err := b.blk.Accept(ctx); err != nil {
		b.log.Debug("failed to accept block during bootstrapping",
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
		return fmt.Errorf("failed to accept block in bootstrapping: %w", err)
	}
	
	return nil
}

func (b *blockJob) Bytes() []byte {
	return b.blk.Bytes()
}

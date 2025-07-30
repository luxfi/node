// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"time"

	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/vms/platformvm/block"

	smblock "github.com/luxfi/node/consensus/engine/chain/block"
)

var (
	_ chain.Block              = (*Block)(nil)
	_ chain.OracleBlock        = (*Block)(nil)
	_ smblock.WithVerifyContext = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	block.Block
	manager *manager
}

func (*Block) ShouldVerifyWithContext(context.Context) (bool, error) {
	return true, nil
}

func (b *Block) VerifyWithContext(ctx context.Context, blockContext *smblock.Context) error {
	blkID := b.Block.ID()
	blkState, previouslyExecuted := b.manager.blkIDToState[blkID]
	warpAlreadyVerified := previouslyExecuted && blkState.verifiedHeights.Contains(blockContext.PChainHeight)

	// If the chain is bootstrapped and the warp messages haven't been verified,
	// we must verify them.
	if !warpAlreadyVerified && b.manager.txExecutorBackend.Bootstrapped.Get() {
		err := VerifyWarpMessages(
			ctx,
			b.manager.ctx.NetworkID,
			b.manager.ctx.ValidatorState,
			blockContext.PChainHeight,
			b.Block,
		)
		if err != nil {
			return err
		}
	}

	// If the block was previously executed, we don't need to execute it again,
	// we can just mark that the warp messages are valid at this height.
	if previouslyExecuted {
		blkState.verifiedHeights.Add(blockContext.PChainHeight)
		return nil
	}

	// Since this is the first time we are verifying this block, we must execute
	// the state transitions to generate the state diffs.
	return b.Visit(&verifier{
		backend:           b.manager.backend,
		txExecutorBackend: b.manager.txExecutorBackend,
		pChainHeight:      blockContext.PChainHeight,
	})
}

func (b *Block) Verify(ctx context.Context) error {
	return b.VerifyWithContext(
		ctx,
		&smblock.Context{
			PChainHeight: 0,
		},
	)
}

func (b *Block) Accept() error {
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject() error {
	return b.Visit(b.manager.rejector)
}

func (b *Block) ID() string {
	return b.Block.ID().String()
}

func (b *Block) Status() choices.Status {
	// TODO: Implement proper status tracking
	return choices.Processing
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.Block.ID())
}

func (b *Block) Time() uint64 {
	return uint64(b.manager.getTimestamp(b.Block.ID()).Unix())
}

func (b *Block) Options(context.Context) ([2]chain.Block, error) {
	options := options{
		log:                     b.manager.ctx.Log,
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

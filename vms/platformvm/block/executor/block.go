// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	platformblock "github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/runtime"
	"github.com/luxfi/vm/chain"
)

var (
	_ chain.Block             = (*Block)(nil)
	_ chain.WithVerifyRuntime = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	platformblock.Block
	manager *manager
}

// ParentID implements chain.Block interface by delegating to Parent()
func (b *Block) ParentID() ids.ID {
	return b.Parent()
}

// Status implements chain.Block interface.
// Block status is tracked by the block state manager; this returns the
// default (processing) status since blocks reaching this code path
// have not yet been accepted or rejected.
func (b *Block) Status() uint8 {
	return 0
}

func (*Block) ShouldVerifyWithRuntime(context.Context) (bool, error) {
	return true, nil
}

func (b *Block) VerifyWithRuntime(ctx context.Context, blockContext *runtime.Runtime) error {
	blkID := b.ID()
	blkState, previouslyExecuted := b.manager.backend.getBlockState(blkID)
	warpAlreadyVerified := previouslyExecuted && blkState.hasVerifiedHeight(blockContext.PChainHeight)

	// If the chain is bootstrapped and the warp messages haven't been verified,
	// we must verify them.
	if !warpAlreadyVerified && b.manager.txExecutorBackend.Bootstrapped.Get() {
		err := VerifyWarpMessages(
			ctx,
			b.manager.rt.NetworkID,
			b.manager.validatorManager,
			blockContext.PChainHeight,
			b,
		)
		if err != nil {
			return err
		}
	}

	// If the block was previously executed, we don't need to execute it again,
	// we can just mark that the warp messages are valid at this height.
	if previouslyExecuted {
		blkState.markVerifiedHeight(blockContext.PChainHeight)
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
	return b.VerifyWithRuntime(
		ctx,
		&runtime.Runtime{
			PChainHeight: 0,
		},
	)
}

func (b *Block) Accept(context.Context) error {
	// Serialize the block's state commit (acceptor → state.Apply/CommitBatch →
	// state.write) with the VM's OTHER writers of the same shared state — the
	// peer Disconnect and Start/StopTracking uptime flushes, which run on
	// goroutines the consensus engine does not serialize against accept (it
	// invokes VM.Accept as a lock-free call-out). Holding the VM's stateLock for
	// the whole visit makes accept atomic w.r.t. those commits, closing the
	// concurrent-map-write in state.write(). Mirrors avalanchego's ctx.Lock,
	// which serializes block accept with engine.Connected/Disconnected.
	b.manager.stateLock.Lock()
	defer b.manager.stateLock.Unlock()
	return b.Visit(b.manager.acceptor)
}

func (b *Block) Reject(context.Context) error {
	// Held under the same lock as Accept so a block DECISION (accept or reject)
	// is uniformly serialized with the VM's peer-lifecycle state commits.
	b.manager.stateLock.Lock()
	defer b.manager.stateLock.Unlock()
	return b.Visit(b.manager.rejector)
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

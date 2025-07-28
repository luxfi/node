// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"

	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/ids"
)

var _ linear.Block = (*Block)(nil)

// Block wraps an Ethereum block to implement the linear.Block interface
type Block struct {
	vm       *VM
	ethBlock *types.Block
	id       ids.ID
	status   choices.Status
}

// newBlock creates a new block wrapper
func (vm *VM) newBlock(ethBlock *types.Block) (*Block, error) {
	if ethBlock == nil {
		return nil, errNilBlock
	}

	// Create block ID from Ethereum block hash
	blockID := ids.ID(ethBlock.Hash())

	return &Block{
		vm:       vm,
		ethBlock: ethBlock,
		id:       blockID,
		status:   choices.Processing,
	}, nil
}

// ID implements the linear.Block interface
func (b *Block) ID() ids.ID {
	return b.id
}

// Accept implements the linear.Block interface
func (b *Block) Accept(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()

	// Mark block as accepted
	b.status = choices.Accepted
	b.vm.lastAccepted = b.id

	// Clean up built blocks
	delete(b.vm.builtBlocks, b.id)

	return nil
}

// Reject implements the linear.Block interface
func (b *Block) Reject(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()

	// Mark block as rejected
	b.status = choices.Rejected

	// Clean up built blocks
	delete(b.vm.builtBlocks, b.id)

	return nil
}

// Status implements the linear.Block interface
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent implements the linear.Block interface
func (b *Block) Parent() ids.ID {
	if b.ethBlock.NumberU64() == 0 {
		// Genesis block has no parent
		return ids.Empty
	}
	return ids.ID(b.ethBlock.ParentHash())
}

// Verify implements the linear.Block interface
func (b *Block) Verify(ctx context.Context) error {
	// Basic verification
	if b.ethBlock == nil {
		return errNilBlock
	}

	// Check if block timestamp is not too far in the future
	if b.ethBlock.Time() > uint64(time.Now().Add(10*time.Second).Unix()) {
		return fmt.Errorf("block timestamp too far in future")
	}

	return nil
}

// Bytes implements the linear.Block interface
func (b *Block) Bytes() []byte {
	bytes, err := rlp.EncodeToBytes(b.ethBlock)
	if err != nil {
		// This should never happen for a valid block
		panic(fmt.Sprintf("failed to RLP encode block: %v", err))
	}
	return bytes
}

// Height implements the linear.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.NumberU64()
}

// Timestamp implements the linear.Block interface
func (b *Block) Timestamp() time.Time {
	return time.Unix(int64(b.ethBlock.Time()), 0)
}
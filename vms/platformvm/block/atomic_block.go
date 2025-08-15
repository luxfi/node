// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "context"

import (
	"fmt"

	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*ApricotAtomicBlock)(nil)

// ApricotAtomicBlock being accepted results in the atomic transaction contained
// in the block to be accepted and committed to the chain.
type ApricotAtomicBlock struct {
	CommonBlock `serialize:"true"`
	Tx          *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ApricotAtomicBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	if err := b.Tx.Initialize(txs.Codec); err != nil {
		return fmt.Errorf("failed to initialize tx: %w", err)
	}
	return nil
}

func (b *ApricotAtomicBlock) InitCtx(ctx context.Context) {
	b.Tx.Unsigned.InitCtx(ctx)
}

func (b *ApricotAtomicBlock) Txs() []*txs.Tx {
	return []*txs.Tx{b.Tx}
}

func (b *ApricotAtomicBlock) Visit(v Visitor) error {
	return v.ApricotAtomicBlock(b)
}

func NewApricotAtomicBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ApricotAtomicBlock, error) {
	blk := &ApricotAtomicBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

// InitializeWithContext initializes the block with consensus context
func (b *ApricotAtomicBlock) InitializeWithContext(ctx context.Context, chainCtx context.Context) error {
    // Initialize any context-dependent fields here
    return nil
}

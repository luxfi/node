// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
)

var (
	_ BanffBlock = (*BanffAbortBlock)(nil)
	_ Block      = (*ApricotAbortBlock)(nil)
)

type BanffAbortBlock struct {
	Time              uint64 `serialize:"true" json:"time"`
	ApricotAbortBlock `serialize:"true"`
}

func (b *BanffAbortBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *BanffAbortBlock) Visit(v Visitor) error {
	return v.BanffAbortBlock(b)
}

func NewBanffAbortBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*BanffAbortBlock, error) {
	blk := &BanffAbortBlock{
		Time: uint64(timestamp.Unix()),
		ApricotAbortBlock: ApricotAbortBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

type ApricotAbortBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotAbortBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

// Initialize implements quasar.ContextInitializable
func (b *ApricotAbortBlock) Initialize(ctx *quasar.Context) error {
	b.InitCtx(ctx)
	return nil
}

// Initialize implements quasar.ContextInitializable
func (b *BanffAbortBlock) Initialize(ctx *quasar.Context) error {
	b.ApricotAbortBlock.Initialize(ctx)
	return nil
}

func (*ApricotAbortBlock) InitCtx(*quasar.Context) {}

func (*ApricotAbortBlock) Txs() []*txs.Tx {
	return nil
}

func (b *ApricotAbortBlock) Visit(v Visitor) error {
	return v.ApricotAbortBlock(b)
}

// NewApricotAbortBlock is kept for testing purposes only.
// Following Banff activation and subsequent code cleanup, Apricot Abort blocks
// should be only verified (upon bootstrap), never created anymore
func NewApricotAbortBlock(
	parentID ids.ID,
	height uint64,
) (*ApricotAbortBlock, error) {
	blk := &ApricotAbortBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

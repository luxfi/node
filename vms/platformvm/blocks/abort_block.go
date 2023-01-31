// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
	return blk, initialize(blk)
}

type ApricotAbortBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotAbortBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
func (*ApricotAbortBlock) InitCtx(*snow.Context) {}

func (*ApricotAbortBlock) Txs() []*txs.Tx {
	return nil
}

func (b *ApricotAbortBlock) Visit(v Visitor) error {
	return v.ApricotAbortBlock(b)
}
<<<<<<< HEAD
=======
func (*ApricotAbortBlock) InitCtx(*snow.Context)   {}
func (*ApricotAbortBlock) Txs() []*txs.Tx          { return nil }
func (b *ApricotAbortBlock) Visit(v Visitor) error { return v.ApricotAbortBlock(b) }
>>>>>>> 3a7ebb1da (Add UnusedParameter linter (#2226))
=======
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))

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
	return blk, initialize(blk)
}

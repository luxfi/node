// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	_ BanffBlock = (*BanffStandardBlock)(nil)
	_ Block      = (*ApricotStandardBlock)(nil)
)

type BanffStandardBlock struct {
	Time                 uint64 `serialize:"true" json:"time"`
	ApricotStandardBlock `serialize:"true"`
}

func (b *BanffStandardBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *BanffStandardBlock) Visit(v Visitor) error {
	return v.BanffStandardBlock(b)
}

func NewBanffStandardBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*BanffStandardBlock, error) {
	blk := &BanffStandardBlock{
		Time: uint64(timestamp.Unix()),
		ApricotStandardBlock: ApricotStandardBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
			Transactions: txs,
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

type ApricotStandardBlock struct {
	CommonBlock  `serialize:"true"`
	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *ApricotStandardBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	for _, tx := range b.Transactions {
		if err := tx.Initialize(txs.Codec); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

// Initialize implements quasar.ContextInitializable
func (b *ApricotStandardBlock) Initialize(ctx *quasar.Context) error {
	b.InitCtx(ctx)
	return nil
}

// Initialize implements quasar.ContextInitializable
func (b *BanffStandardBlock) Initialize(ctx *quasar.Context) error {
	b.InitCtx(ctx)
	return nil
}

func (b *ApricotStandardBlock) InitCtx(ctx *quasar.Context) {
	for _, tx := range b.Transactions {
		tx.Unsigned.Initialize(ctx)
	}
}

func (b *ApricotStandardBlock) Txs() []*txs.Tx {
	return b.Transactions
}

func (b *ApricotStandardBlock) Visit(v Visitor) error {
	return v.ApricotStandardBlock(b)
}

// NewApricotStandardBlock is kept for testing purposes only.
// Following Banff activation and subsequent code cleanup, Apricot Standard blocks
// should be only verified (upon bootstrap), never created anymore
func NewApricotStandardBlock(
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*ApricotStandardBlock, error) {
	blk := &ApricotStandardBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Transactions: txs,
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

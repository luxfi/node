// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/runtime"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*StandardBlock)(nil)

// StandardBlock is the canonical P-Chain standard block. It carries a
// per-block timestamp (advance-all-implicitly removed the separate
// AdvanceTimeTx flow) and an ordered list of decision txs.
type StandardBlock struct {
	Time         uint64    `serialize:"true" json:"time"`
	CommonBlock  `serialize:"true"`
	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *StandardBlock) Timestamp() time.Time { return time.Unix(int64(b.Time), 0) }

func (b *StandardBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	for _, tx := range b.Transactions {
		if err := tx.Initialize(txs.Codec); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (b *StandardBlock) InitRuntime(rt *runtime.Runtime) {
	for _, tx := range b.Transactions {
		tx.Unsigned.InitRuntime(rt)
	}
}

func (b *StandardBlock) Txs() []*txs.Tx          { return b.Transactions }
func (b *StandardBlock) Visit(v Visitor) error   { return v.StandardBlock(b) }
func (*StandardBlock) Initialize(context.Context) error { return nil }

func NewStandardBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*StandardBlock, error) {
	blk := &StandardBlock{
		Time:         uint64(timestamp.Unix()),
		CommonBlock:  CommonBlock{PrntID: parentID, Hght: height},
		Transactions: txs,
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

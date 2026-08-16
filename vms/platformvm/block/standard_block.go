// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*StandardBlock)(nil)

// StandardBlock is the canonical P-Chain standard block. It carries a
// per-block timestamp and an ordered list of decision txs, stored as a u32
// length list + concatenated tx blob in the zap buffer.
type StandardBlock struct {
	commonZapBlock
}

// DecisionTxs returns the decision txs in wire order. A standard block carries
// nothing else.
func (b *StandardBlock) DecisionTxs() []*txs.Tx {
	list, _ := readTxList(b.msg.Root(), offBlkTxLengths, offBlkTxBlob)
	return list
}

func (b *StandardBlock) Visit(v Visitor) error { return v.StandardBlock(b) }

func NewStandardBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	txs []*txs.Tx,
) (*StandardBlock, error) {
	bytes, err := buildBlock(blkStandard, parentID, height, uint64(timestamp.Unix()), txs, nil)
	if err != nil {
		return nil, err
	}
	blk := &StandardBlock{}
	if err := blk.setID(bytes); err != nil {
		return nil, err
	}
	return blk, nil
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*AbortBlock)(nil)

// AbortBlock is the canonical P-Chain abort outcome of a ProposalBlock. It
// carries a timestamp and no txs; the struct is the zap buffer.
type AbortBlock struct {
	commonZapBlock
}

func (*AbortBlock) DecisionTxs() []*txs.Tx  { return nil }
func (b *AbortBlock) Visit(v Visitor) error { return v.AbortBlock(b) }

func NewAbortBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*AbortBlock, error) {
	bytes, err := buildBlock(blkAbort, parentID, height, uint64(timestamp.Unix()), nil, nil)
	if err != nil {
		return nil, err
	}
	blk := &AbortBlock{}
	if err := blk.setID(bytes); err != nil {
		return nil, err
	}
	return blk, nil
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*CommitBlock)(nil)

// CommitBlock is the canonical P-Chain commit outcome of a ProposalBlock. It
// carries a timestamp and no txs; the struct is the zap buffer.
type CommitBlock struct {
	commonZapBlock
}

func (*CommitBlock) Txs() []*txs.Tx          { return nil }
func (b *CommitBlock) Visit(v Visitor) error { return v.CommitBlock(b) }

func NewCommitBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*CommitBlock, error) {
	bytes, err := buildBlock(blkCommit, parentID, height, uint64(timestamp.Unix()), nil, nil)
	if err != nil {
		return nil, err
	}
	blk := &CommitBlock{}
	if err := blk.setID(bytes); err != nil {
		return nil, err
	}
	return blk, nil
}

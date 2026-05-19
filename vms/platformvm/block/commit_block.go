// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"time"

	"github.com/luxfi/runtime"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*CommitBlock)(nil)

// CommitBlock is the canonical P-Chain commit outcome of a ProposalBlock.
type CommitBlock struct {
	Time        uint64 `serialize:"true" json:"time"`
	CommonBlock `serialize:"true"`
}

func (b *CommitBlock) Timestamp() time.Time { return time.Unix(int64(b.Time), 0) }

func (b *CommitBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

func (*CommitBlock) InitRuntime(*runtime.Runtime) {}
func (*CommitBlock) Txs() []*txs.Tx                 { return nil }
func (b *CommitBlock) Visit(v Visitor) error        { return v.CommitBlock(b) }
func (*CommitBlock) Initialize(context.Context) error { return nil }

func NewCommitBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*CommitBlock, error) {
	blk := &CommitBlock{
		Time:        uint64(timestamp.Unix()),
		CommonBlock: CommonBlock{PrntID: parentID, Hght: height},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

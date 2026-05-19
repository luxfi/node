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

var _ Block = (*AbortBlock)(nil)

// AbortBlock is the canonical P-Chain abort outcome of a ProposalBlock.
type AbortBlock struct {
	Time        uint64 `serialize:"true" json:"time"`
	CommonBlock `serialize:"true"`
}

func (b *AbortBlock) Timestamp() time.Time { return time.Unix(int64(b.Time), 0) }

func (b *AbortBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

func (*AbortBlock) InitRuntime(*runtime.Runtime) {}
func (*AbortBlock) Txs() []*txs.Tx                { return nil }
func (b *AbortBlock) Visit(v Visitor) error       { return v.AbortBlock(b) }
func (*AbortBlock) Initialize(context.Context) error { return nil }

func NewAbortBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*AbortBlock, error) {
	blk := &AbortBlock{
		Time:        uint64(timestamp.Unix()),
		CommonBlock: CommonBlock{PrntID: parentID, Hght: height},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

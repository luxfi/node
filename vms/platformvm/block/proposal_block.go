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

var _ Block = (*ProposalBlock)(nil)

// ProposalBlock is the canonical P-Chain proposal block. It carries a
// per-block timestamp, a single proposal Tx, and a tail of decision Txs that
// commit atomically with the proposal outcome.
type ProposalBlock struct {
	Time         uint64    `serialize:"true" json:"time"`
	Transactions []*txs.Tx `serialize:"true" json:"txs"`
	CommonBlock  `serialize:"true"`
	Tx           *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ProposalBlock) Timestamp() time.Time { return time.Unix(int64(b.Time), 0) }

func (b *ProposalBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	if err := b.Tx.Initialize(txs.Codec); err != nil {
		return fmt.Errorf("failed to initialize tx: %w", err)
	}
	for _, tx := range b.Transactions {
		if err := tx.Initialize(txs.Codec); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (b *ProposalBlock) InitRuntime(rt *runtime.Runtime) {
	b.Tx.Unsigned.InitRuntime(rt)
	for _, tx := range b.Transactions {
		tx.Unsigned.InitRuntime(rt)
	}
}

func (b *ProposalBlock) Txs() []*txs.Tx {
	l := len(b.Transactions)
	out := make([]*txs.Tx, l+1)
	copy(out, b.Transactions)
	out[l] = b.Tx
	return out
}

func (b *ProposalBlock) Visit(v Visitor) error          { return v.ProposalBlock(b) }
func (*ProposalBlock) Initialize(context.Context) error { return nil }

func NewProposalBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	proposalTx *txs.Tx,
	decisionTxs []*txs.Tx,
) (*ProposalBlock, error) {
	blk := &ProposalBlock{
		Time:         uint64(timestamp.Unix()),
		Transactions: decisionTxs,
		CommonBlock:  CommonBlock{PrntID: parentID, Hght: height},
		Tx:           proposalTx,
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ Block = (*ProposalBlock)(nil)

// ProposalBlock is the canonical P-Chain proposal block. It carries a
// per-block timestamp, a tail of decision txs (stored as the u32 length list +
// blob), and a single proposal Tx that commits atomically with them.
type ProposalBlock struct {
	commonZapBlock
}

// Tx returns the single proposal tx.
func (b *ProposalBlock) Tx() *txs.Tx {
	tx, _ := readProposalTx(b.msg.Root())
	return tx
}

// DecisionTxs returns only the decision txs — the ones submitted to this chain
// and paid for by whoever submitted them.
//
// The proposal Tx is deliberately absent. It is emitted by the chain itself, it
// is charged no fee by either calculator, and it is executed by its own path
// (ProposalTx). Handing it to anything that prices transactions asks what a
// chain-emitted tx costs, which is a question with no answer: the complexity
// visitor refuses it, and the refusal reads as a block that cannot be verified.
func (b *ProposalBlock) DecisionTxs() []*txs.Tx {
	decision, _ := readTxList(b.msg.Root(), offBlkTxLengths, offBlkTxBlob)
	return decision
}

// Txs returns the decision txs followed by the proposal Tx (last), matching
// the canonical block ordering.
//
// This is the whole block's contents, for readers that mean exactly that —
// metrics, warp verification, rejection. A reader that means "the txs this
// block charges for" wants DecisionTxs.
func (b *ProposalBlock) Txs() []*txs.Tx {
	decision := b.DecisionTxs()
	proposal := b.Tx()
	if proposal == nil {
		return decision
	}
	out := make([]*txs.Tx, len(decision)+1)
	copy(out, decision)
	out[len(decision)] = proposal
	return out
}

func (b *ProposalBlock) Visit(v Visitor) error { return v.ProposalBlock(b) }

func NewProposalBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	proposalTx *txs.Tx,
	decisionTxs []*txs.Tx,
) (*ProposalBlock, error) {
	bytes, err := buildBlock(blkProposal, parentID, height, uint64(timestamp.Unix()), decisionTxs, proposalTx)
	if err != nil {
		return nil, err
	}
	blk := &ProposalBlock{}
	if err := blk.setID(bytes); err != nil {
		return nil, err
	}
	return blk, nil
}

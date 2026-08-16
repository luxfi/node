// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	_ Block = (*ProposalBlock)(nil)

	// errNoProposalTx rejects a proposal block whose proposal slot is empty or
	// unreadable. The proposal tx is what makes the block a proposal, and the
	// executor has no path that runs without one, so a block missing it is
	// refused where it enters — built or parsed — rather than surfacing as a
	// nil in a reader that has no answer for one.
	errNoProposalTx = errors.New("proposal block without a proposal tx")
)

// ProposalBlock is the canonical P-Chain proposal block. It carries a
// per-block timestamp, a tail of decision txs (stored as the u32 length list +
// blob), and a single proposal Tx that commits atomically with them.
type ProposalBlock struct {
	commonZapBlock
}

// Tx returns the proposal tx. Never nil: a block is only made with a proposal
// slot that carries bytes, and only parsed if that slot reads back. Singular by
// type, so it cannot join a list on its way to a fee calculator or a mempool.
func (b *ProposalBlock) Tx() *txs.Tx {
	tx, _ := readProposalTx(b.msg.Root())
	return tx
}

// DecisionTxs returns the decision txs in wire order. The proposal tx is not
// among them; it runs on its own path, unpriced, and is reached through Tx.
func (b *ProposalBlock) DecisionTxs() []*txs.Tx {
	decision, _ := readTxList(b.msg.Root(), offBlkTxLengths, offBlkTxBlob)
	return decision
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

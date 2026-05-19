// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/block"
	vmcore "github.com/luxfi/vm"
)

var _ block.Visitor = (*rejector)(nil)

// rejector handles the logic for rejecting a block.
// All errors returned by this struct are fatal and should result in the chain
// being shutdown.
type rejector struct {
	*backend
	toEngine        chan<- vmcore.Message
	addTxsToMempool bool
}

func (r *rejector) AbortBlock(b *block.AbortBlock) error {
	return r.rejectBlock(b, "abort")
}

func (r *rejector) CommitBlock(b *block.CommitBlock) error {
	return r.rejectBlock(b, "commit")
}

func (r *rejector) ProposalBlock(b *block.ProposalBlock) error {
	return r.rejectBlock(b, "proposal")
}

func (r *rejector) StandardBlock(b *block.StandardBlock) error {
	return r.rejectBlock(b, "standard")
}

func (r *rejector) rejectBlock(b block.Block, blockType string) error {
	blkID := b.ID()
	defer r.free(blkID)

	log.Debug(
		"rejecting block",
		"blockType", blockType,
		"blkID", blkID,
		"height", b.Height(),
		"parentID", b.Parent(),
	)

	if !r.addTxsToMempool {
		return nil
	}

	for _, tx := range b.Txs() {
		if err := r.Mempool.Add(tx); err != nil {
			log.Debug(
				"failed to reissue tx",
				"txID", tx.ID(),
				"blkID", blkID,
				"error", err,
			)
		}
	}

	if r.Mempool.Len() == 0 {
		return nil
	}

	select {
	case r.toEngine <- vmcore.Message{Type: vmcore.PendingTxs}:
	default:
	}

	return nil
}

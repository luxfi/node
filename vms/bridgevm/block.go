// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bridgevm

import (
	"context"
	"errors"
	"time"

		ethcommon "github.com/ethereum/go-ethereum/common"
		"github.com/ethereum/go-ethereum/core/types"
	
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/utils/hashing"
)

var (
	_ snowman.Block = (*Block)(nil)

	errInvalidBlock = errors.New("invalid block")
)

// Block represents a block in the Bridge Chain
type Block struct {
	vm *VM

	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp time.Time
	
	// Bridge-specific block data
	ethHeaders   []*types.Header
	atomicTxs    []*AtomicTxUpdate
	stateRoots   []*StateRootSubmission
	
	status    choices.Status
	bytes     []byte
}

// AtomicTxUpdate represents an update to an atomic transaction
type AtomicTxUpdate struct {
	TxID        ids.ID         `json:"txId"`
	NewStatus   AtomicTxStatus `json:"newStatus"`
	SubTxUpdate *SubTxUpdate   `json:"subTxUpdate,omitempty"`
}

// SubTxUpdate represents an update to a sub-transaction
type SubTxUpdate struct {
	ChainID ids.ID   `json:"chainId"`
	Index   int      `json:"index"`
	Status  TxStatus `json:"status"`
	Result  []byte   `json:"result,omitempty"`
}

// StateRootSubmission represents a state root submission to Ethereum
type StateRootSubmission struct {
	ChainID      ids.ID         `json:"chainId"`
	BlockHeight  uint64         `json:"blockHeight"`
	StateRoot    ethcommon.Hash `json:"stateRoot"`
	BlockHash    ids.ID         `json:"blockHash"`
	EthTxHash    ethcommon.Hash `json:"ethTxHash,omitempty"`
	Submitted    bool        `json:"submitted"`
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID {
	return b.id
}

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	b.status = choices.Accepted
	
	// Process accepted Ethereum headers
	b.vm.headerMu.Lock()
	for _, header := range b.ethHeaders {
		b.vm.ethHeaders[header.Hash()] = header
	}
	b.vm.headerMu.Unlock()
	
	// Process atomic transaction updates
	b.vm.atomicMu.Lock()
	for _, update := range b.atomicTxs {
		if tx, exists := b.vm.atomicTxs[update.TxID]; exists {
			tx.Status = update.NewStatus
			if update.SubTxUpdate != nil && update.SubTxUpdate.Index < len(tx.SubTxs) {
				tx.SubTxs[update.SubTxUpdate.Index].Status = update.SubTxUpdate.Status
				tx.SubTxs[update.SubTxUpdate.Index].Result = update.SubTxUpdate.Result
			}
			
			// Check if all sub-txs are complete
			allComplete := true
			anyFailed := false
			for _, subTx := range tx.SubTxs {
				if subTx.Status == TxPending {
					allComplete = false
					break
				}
				if subTx.Status == TxFailed {
					anyFailed = true
				}
			}
			
			if allComplete {
				if anyFailed {
					tx.Status = AtomicAborted
				} else {
					tx.Status = AtomicCommitted
				}
				tx.CompletedAt = b.timestamp.Unix()
			}
		}
	}
	b.vm.atomicMu.Unlock()
	
	// Update last accepted
	b.vm.preferredID = b.id
	
	return nil
}

// Reject implements the snowman.Block interface
func (b *Block) Reject(context.Context) error {
	b.status = choices.Rejected
	return nil
}

// Status implements the snowman.Block interface
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() ids.ID {
	return b.parentID
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.height
}

// Timestamp implements the snowman.Block interface
func (b *Block) Timestamp() time.Time {
	return b.timestamp
}

// Verify implements the snowman.Block interface
func (b *Block) Verify(ctx context.Context) error {
	// Verify block structure
	if b.height == 0 && b.parentID != ids.Empty {
		return errInvalidBlock
	}
	
	// Verify Ethereum headers
	for _, header := range b.ethHeaders {
		// TODO: Verify header chain continuity
		if header == nil {
			return errors.New("nil ethereum header")
		}
	}
	
	// Verify atomic transaction updates
	for _, update := range b.atomicTxs {
		if _, exists := b.vm.atomicTxs[update.TxID]; !exists {
			return errors.New("update for unknown atomic transaction")
		}
	}
	
	// Verify state root submissions
	for _, submission := range b.stateRoots {
		if submission.ChainID == ids.Empty {
			return errors.New("invalid chain ID in state root submission")
		}
	}
	
	b.status = choices.Processing
	return nil
}

// Bytes implements the snowman.Block interface
func (b *Block) Bytes() []byte {
	if b.bytes == nil {
		// TODO: Implement proper serialization
		b.bytes = hashing.ComputeHash256([]byte(b.id.String()))
	}
	return b.bytes
}
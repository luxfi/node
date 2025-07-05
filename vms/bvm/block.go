// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils"
)

// Block represents a block in the Bridge chain
type Block struct {
	ParentID        ids.ID            `json:"parentId"`
	BlockHeight     uint64            `json:"height"`
	BlockTimestamp  int64             `json:"timestamp"`
	BridgeRequests  []*BridgeRequest  `json:"bridgeRequests"`
	
	// MPC signatures for this block
	MPCSignatures   map[ids.NodeID][]byte `json:"mpcSignatures"`
	
	// Cached values
	ID_      ids.ID
	bytes    []byte
	status   choices.Status
	vm       *VM
}

var (
	errInvalidBlock = errors.New("invalid block")
	errFutureBlock  = errors.New("block timestamp is in the future")
	
	maxClockSkew = int64(60) // 60 seconds
)

// ID returns the block ID
func (b *Block) ID() ids.ID {
	if b.ID_ == ids.Empty {
		b.ID_ = b.computeID()
	}
	return b.ID_
}

// computeID calculates the block ID
func (b *Block) computeID() ids.ID {
	h := sha256.New()
	
	h.Write(b.ParentID[:])
	
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, b.BlockHeight)
	h.Write(heightBytes)
	
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(b.BlockTimestamp))
	h.Write(timestampBytes)
	
	// Include bridge requests in hash
	for _, req := range b.BridgeRequests {
		h.Write(req.ID[:])
	}
	
	return ids.ID(h.Sum(nil))
}

// Accept marks the block as accepted
func (b *Block) Accept(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()
	
	// Process bridge requests
	for _, req := range b.BridgeRequests {
		// Update bridge registry
		b.vm.bridgeRegistry.mu.Lock()
		
		completed := &CompletedBridge{
			RequestID:    req.ID,
			SourceTxID:   req.SourceTxID,
			CompletedAt:  time.Now(),
		}
		
		// Collect MPC signatures
		if len(req.MPCSignatures) > 0 {
			completed.MPCSignature = req.MPCSignatures[0] // Use aggregated signature
		}
		
		b.vm.bridgeRegistry.CompletedBridges[req.ID] = completed
		
		// Update daily volume
		volume := b.vm.bridgeRegistry.DailyVolume[req.DestChain]
		b.vm.bridgeRegistry.DailyVolume[req.DestChain] = volume + req.Amount
		
		b.vm.bridgeRegistry.mu.Unlock()
		
		// Remove from pending
		delete(b.vm.pendingBridges, req.ID)
		
		b.vm.log.Info("completed bridge request",
			zap.Stringer("requestID", req.ID),
			zap.String("destChain", req.DestChain),
			zap.Uint64("amount", req.Amount),
		)
	}
	
	// Update state
	b.status = choices.Accepted
	b.vm.lastAcceptedID = b.ID()
	
	// Remove from pending blocks
	delete(b.vm.pendingBlocks, b.ID())
	
	// Persist block
	return b.vm.putBlock(b)
}

// Reject marks the block as rejected
func (b *Block) Reject(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()
	
	b.status = choices.Rejected
	delete(b.vm.pendingBlocks, b.ID())
	
	return nil
}

// Status returns the block's status
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent returns the parent block ID
func (b *Block) Parent() ids.ID {
	return b.ParentID
}

// Verify verifies the block
func (b *Block) Verify(ctx context.Context) error {
	// Basic validation
	if b.BlockHeight == 0 && b.ParentID != ids.Empty {
		return errInvalidBlock
	}
	
	// Verify timestamp
	if b.BlockTimestamp > time.Now().Unix()+maxClockSkew {
		return errFutureBlock
	}
	
	// Verify each bridge request
	for _, req := range b.BridgeRequests {
		// Verify request has enough confirmations
		if req.Confirmations < b.vm.config.MinConfirmations {
			return fmt.Errorf("insufficient confirmations for request %s: %d < %d", 
				req.ID, req.Confirmations, b.vm.config.MinConfirmations)
		}
		
		// Verify amount doesn't exceed limits
		if req.Amount > b.vm.config.MaxBridgeAmount {
			return fmt.Errorf("bridge amount exceeds maximum: %d > %d",
				req.Amount, b.vm.config.MaxBridgeAmount)
		}
		
		// Verify daily limit
		b.vm.bridgeRegistry.mu.RLock()
		dailyVolume := b.vm.bridgeRegistry.DailyVolume[req.DestChain]
		b.vm.bridgeRegistry.mu.RUnlock()
		
		if dailyVolume + req.Amount > b.vm.config.DailyBridgeLimit {
			return fmt.Errorf("would exceed daily bridge limit for chain %s", req.DestChain)
		}
		
		// Verify MPC signatures if present
		if len(req.MPCSignatures) > 0 {
			// TODO: Implement MPC signature verification
			// This would verify that t+1 parties have signed
		}
	}
	
	// Verify MPC block signatures
	validSignatures := 0
	for nodeID, sig := range b.MPCSignatures {
		party, exists := b.vm.mpcParties[nodeID]
		if !exists {
			continue
		}
		
		// Verify signature
		blockHash := b.ID()
		if err := party.VerifySignature(blockHash[:], sig); err == nil {
			validSignatures++
		}
	}
	
	// Require t+1 valid signatures
	if validSignatures < b.vm.config.MPCThreshold + 1 {
		return fmt.Errorf("insufficient MPC signatures: %d < %d",
			validSignatures, b.vm.config.MPCThreshold + 1)
	}
	
	return nil
}

// Height returns the block height
func (b *Block) Height() uint64 {
	return b.BlockHeight
}

// Timestamp returns the block timestamp
func (b *Block) Timestamp() time.Time {
	return time.Unix(b.BlockTimestamp, 0)
}

// Bytes returns the block bytes
func (b *Block) Bytes() []byte {
	if b.bytes != nil {
		return b.bytes
	}
	
	bytes, err := utils.Codec.Marshal(codecVersion, b)
	if err != nil {
		return nil
	}
	
	b.bytes = bytes
	return bytes
}
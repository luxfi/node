// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package yvm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils"
)

// Block represents a block in the Y-Chain quantum checkpoint ledger
type Block struct {
	ParentID        ids.ID          `json:"parentId"`
	BlockHeight     uint64          `json:"height"`
	BlockTimestamp  int64           `json:"timestamp"`
	Epoch           uint64          `json:"epoch"`
	EpochRoots      []*EpochRootTx  `json:"epochRoots"`
	
	// Cached values
	ID_      ids.ID
	bytes    []byte
	status   choices.Status
	vm       *VM
}

var (
	errInvalidBlock    = errors.New("invalid block")
	errFutureBlock     = errors.New("block timestamp is in the future")
	errInvalidEpoch    = errors.New("invalid epoch number")
	errNoEpochRoots    = errors.New("block must contain at least one epoch root")
	
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
	
	epochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBytes, b.Epoch)
	h.Write(epochBytes)
	
	// Include epoch roots in hash
	for _, root := range b.EpochRoots {
		h.Write([]byte(fmt.Sprintf("%d", root.Epoch)))
		h.Write(root.NodeID[:])
		h.Write(root.SPHINCSig)
	}
	
	return ids.ID(h.Sum(nil))
}

// Accept marks the block as accepted
func (b *Block) Accept(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()
	
	// Process epoch checkpoint
	if len(b.EpochRoots) > 0 {
		epochRoot := b.EpochRoots[0] // We only store one per block
		
		// Create epoch checkpoint
		checkpoint := &EpochCheckpoint{
			Epoch:            epochRoot.Epoch,
			Timestamp:        b.BlockTimestamp,
			ChainRoots:       epochRoot.ChainRoots,
			EpochRootHash:    computeEpochRootHash(epochRoot.Epoch, epochRoot.ChainRoots),
			SPHINCSSignature: epochRoot.SPHINCSig,
		}
		
		// Store checkpoint
		b.vm.epochCheckpoints[epochRoot.Epoch] = checkpoint
		
		// Update current epoch
		b.vm.currentEpoch = epochRoot.Epoch
		
		// Anchor to Bitcoin if enabled
		if b.vm.config.BitcoinEnabled {
			go b.anchorToBitcoin(checkpoint)
		}
		
		// Archive to IPFS if enabled
		if b.vm.config.IPFSEnabled {
			go b.archiveToIPFS(checkpoint)
		}
		
		// Clean up old pending signatures
		b.vm.sphincsAggregator.mu.Lock()
		delete(b.vm.sphincsAggregator.pendingSigs, epochRoot.Epoch)
		// Clean up old epochs (keep last 100)
		for epoch := range b.vm.sphincsAggregator.pendingSigs {
			if epoch < epochRoot.Epoch-100 {
				delete(b.vm.sphincsAggregator.pendingSigs, epoch)
			}
		}
		b.vm.sphincsAggregator.mu.Unlock()
		
		b.vm.log.Info("accepted Y-Chain epoch checkpoint",
			zap.Uint64("epoch", epochRoot.Epoch),
			zap.Stringer("blockID", b.ID()),
			zap.Int("chainCount", len(epochRoot.ChainRoots)),
		)
		
		// Check for divergence (slashing condition)
		if b.vm.config.EnableSlashing {
			go b.checkForDivergence(checkpoint)
		}
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
	
	// Must have exactly one epoch root per block
	if len(b.EpochRoots) != 1 {
		return errNoEpochRoots
	}
	
	epochRoot := b.EpochRoots[0]
	
	// Verify epoch progression
	if b.BlockHeight > 0 {
		parent, err := b.vm.getBlock(b.ParentID)
		if err != nil {
			return fmt.Errorf("failed to get parent block: %w", err)
		}
		
		if epochRoot.Epoch <= parent.Epoch {
			return fmt.Errorf("epoch must increase: %d <= %d", epochRoot.Epoch, parent.Epoch)
		}
	}
	
	// Verify epoch root transaction
	if err := b.verifyEpochRoot(epochRoot); err != nil {
		return fmt.Errorf("invalid epoch root: %w", err)
	}
	
	// Verify block size
	blockBytes, err := b.Bytes()
	if err != nil {
		return err
	}
	
	if len(blockBytes) > maxBlockSize {
		return fmt.Errorf("block size %d exceeds maximum %d", len(blockBytes), maxBlockSize)
	}
	
	return nil
}

// verifyEpochRoot verifies an epoch root transaction
func (b *Block) verifyEpochRoot(tx *EpochRootTx) error {
	// Verify chain count
	if len(tx.ChainRoots) == 0 {
		return errors.New("no chain roots provided")
	}
	
	if len(tx.ChainRoots) > maxChains {
		return fmt.Errorf("too many chain roots: %d > %d", len(tx.ChainRoots), maxChains)
	}
	
	// Verify each chain root
	for chainID, root := range tx.ChainRoots {
		if len(root) != maxRootSize {
			return fmt.Errorf("invalid root size for chain %s: %d != %d", 
				chainID, len(root), maxRootSize)
		}
		
		// Verify chain is tracked
		tracked := false
		for _, trackedChain := range b.vm.config.TrackedChains {
			if chainID == trackedChain {
				tracked = true
				break
			}
		}
		if !tracked {
			return fmt.Errorf("untracked chain: %s", chainID)
		}
	}
	
	// Compute epoch root hash
	epochRootHash := computeEpochRootHash(tx.Epoch, tx.ChainRoots)
	
	// Verify SPHINCS+ signature
	b.vm.sphincsAggregator.mu.RLock()
	pubKey, exists := b.vm.sphincsAggregator.publicKeys[tx.NodeID]
	b.vm.sphincsAggregator.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("unknown node ID: %s", tx.NodeID)
	}
	
	if !verifySPHINCS(pubKey, epochRootHash, tx.SPHINCSig) {
		return errors.New("invalid SPHINCS+ signature")
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

// anchorToBitcoin anchors the epoch hash to Bitcoin via OP_RETURN
func (b *Block) anchorToBitcoin(checkpoint *EpochCheckpoint) {
	// TODO: Implement Bitcoin anchoring
	// This would:
	// 1. Connect to Bitcoin RPC
	// 2. Create OP_RETURN transaction with epoch hash
	// 3. Broadcast transaction
	// 4. Update checkpoint with Bitcoin txid
	
	b.vm.log.Info("anchoring to Bitcoin",
		zap.Uint64("epoch", checkpoint.Epoch),
		zap.String("hash", fmt.Sprintf("%x", checkpoint.EpochRootHash[:8])),
	)
}

// archiveToIPFS archives the checkpoint to IPFS
func (b *Block) archiveToIPFS(checkpoint *EpochCheckpoint) {
	// TODO: Implement IPFS archival
	// This would:
	// 1. Connect to IPFS gateway
	// 2. Upload checkpoint data
	// 3. Update checkpoint with IPFS hash
	
	b.vm.log.Info("archiving to IPFS",
		zap.Uint64("epoch", checkpoint.Epoch),
	)
}

// checkForDivergence checks for chain divergence to trigger slashing
func (b *Block) checkForDivergence(checkpoint *EpochCheckpoint) {
	// TODO: Implement divergence detection
	// This would:
	// 1. Compare roots with previous epochs
	// 2. Detect if any chain has forked after being checkpointed
	// 3. Generate proof-of-divergence
	// 4. Trigger slashing if detected
	
	b.vm.log.Debug("checking for chain divergence",
		zap.Uint64("epoch", checkpoint.Epoch),
	)
}
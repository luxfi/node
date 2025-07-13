// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zvm

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/utils/logging"
)

// StateTree manages the Merkle tree of the UTXO set
type StateTree struct {
	db     database.Database
	log    logging.Logger
	
	// Current state
	currentRoot []byte
	treeHeight  int
	
	// Pending changes
	pendingAdds    [][]byte
	pendingRemoves [][]byte
	
	mu sync.RWMutex
}

// NewStateTree creates a new state tree
func NewStateTree(db database.Database, log logging.Logger) (*StateTree, error) {
	st := &StateTree{
		db:          db,
		log:         log,
		treeHeight:  32, // 32 levels for 2^32 leaves
		currentRoot: make([]byte, 32),
	}
	
	// Initialize with empty tree root
	emptyRoot := sha256.Sum256([]byte("empty_tree"))
	st.currentRoot = emptyRoot[:]
	
	return st, nil
}

// ApplyTransaction applies a transaction to the state tree
func (st *StateTree) ApplyTransaction(tx *Transaction) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	// Remove spent UTXOs (nullifiers)
	for _, nullifier := range tx.Nullifiers {
		st.pendingRemoves = append(st.pendingRemoves, nullifier)
	}
	
	// Add new UTXOs (output commitments)
	for _, output := range tx.Outputs {
		st.pendingAdds = append(st.pendingAdds, output.Commitment)
	}
	
	return nil
}

// ComputeRoot computes the new Merkle root after pending changes
func (st *StateTree) ComputeRoot() ([]byte, error) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	
	// In production, this would compute the actual Merkle tree root
	// For now, we compute a simple hash of all changes
	
	h := sha256.New()
	h.Write(st.currentRoot)
	
	// Include additions
	for _, add := range st.pendingAdds {
		h.Write(add)
	}
	
	// Include removals
	for _, remove := range st.pendingRemoves {
		h.Write(remove)
	}
	
	return h.Sum(nil), nil
}

// Finalize commits the pending changes and updates the root
func (st *StateTree) Finalize(newRoot []byte) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	// Update root
	st.currentRoot = newRoot
	
	// Clear pending changes
	st.pendingAdds = nil
	st.pendingRemoves = nil
	
	// Save root to database
	if err := st.db.Put([]byte("state_root"), newRoot); err != nil {
		return err
	}
	
	st.log.Debug("State tree finalized",
		zap.String("root", fmt.Sprintf("%x", newRoot[:8])),
		zap.Int("adds", len(st.pendingAdds)),
		zap.Int("removes", len(st.pendingRemoves)),
	)
	
	return nil
}

// GetRoot returns the current state root
func (st *StateTree) GetRoot() []byte {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.currentRoot
}

// GetMerkleProof generates a Merkle proof for a commitment
func (st *StateTree) GetMerkleProof(commitment []byte) ([][]byte, error) {
	// In production, this would generate an actual Merkle proof
	// For now, return a dummy proof
	proof := make([][]byte, st.treeHeight)
	for i := 0; i < st.treeHeight; i++ {
		proof[i] = make([]byte, 32)
	}
	return proof, nil
}

// VerifyMerkleProof verifies a Merkle proof
func (st *StateTree) VerifyMerkleProof(commitment []byte, proof [][]byte, root []byte) bool {
	// In production, this would verify the actual Merkle proof
	// For now, return true if proof has correct length
	return len(proof) == st.treeHeight
}

// Close closes the state tree
func (st *StateTree) Close() {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	st.pendingAdds = nil
	st.pendingRemoves = nil
}
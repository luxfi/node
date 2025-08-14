// Package engine provides VM consensus integration
package engine

import (
	"context"
	"errors"
	
	"github.com/luxfi/consensus/dag/witness"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/trie"
	"github.com/luxfi/geth/trie/utils"
	"github.com/luxfi/geth/triedb/database"
	"github.com/luxfi/ids"
)

// VerkleVMIntegration bridges VMs with Verkle+FPC consensus
type VerkleVMIntegration struct {
	engine *WaveFPCEngine
	db     database.NodeDatabase
	cache  *utils.PointCache
}

// NewVerkleVMIntegration creates VM-consensus bridge
func NewVerkleVMIntegration(engine *WaveFPCEngine, db database.NodeDatabase) *VerkleVMIntegration {
	return &VerkleVMIntegration{
		engine: engine,
		db:     db,
		cache:  utils.NewPointCache(10000),
	}
}

// ProcessBlock processes a block through consensus with Verkle validation
func (v *VerkleVMIntegration) ProcessBlock(ctx context.Context, blkID ids.ID, txs []Transaction) error {
	
	// Process each transaction through consensus
	for _, tx := range txs {
		txID := ids.ID(tx.Hash())
		
		// Propose to consensus
		if err := v.engine.Propose(ctx, txID); err != nil {
			return err
		}
		
		// Validate Verkle witness if present
		if witness := tx.Witness(); witness != nil {
			valid, size, root := v.engine.ValidateWitness(
				mockHeader{id: blockIDFromHash(tx.Hash())},
				witness,
			)
			if !valid {
				return errors.New("invalid Verkle witness")
			}
			v.engine.log.Debug("Validated Verkle witness", 
				"size", size, 
				"root", root,
			)
		}
	}
	
	// Wait for consensus on all transactions
	for _, tx := range txs {
		txID := ids.ID(tx.Hash())
		accepted, err := v.engine.Query(ctx, txID)
		if err != nil {
			return err
		}
		if !accepted {
			return errors.New("transaction rejected by consensus")
		}
	}
	
	return nil
}

// GetVerkleTrie returns a Verkle trie for state access
func (v *VerkleVMIntegration) GetVerkleTrie(root common.Hash) (*trie.VerkleTrie, error) {
	return trie.NewVerkleTrie(root, v.db, v.cache)
}

// Transaction represents a VM transaction with optional witness
type Transaction interface {
	Hash() common.Hash
	Witness() []byte
}

type mockHeader struct {
	id witness.BlockID
}

func (h mockHeader) ID() witness.BlockID           { return h.id }
func (h mockHeader) Round() uint64                 { return 0 }
func (h mockHeader) Parents() []witness.BlockID    { return nil }
func (h mockHeader) WitnessRoot() [32]byte        { return [32]byte{} }

func blockIDFromHash(h common.Hash) witness.BlockID {
	var id witness.BlockID
	copy(id[:], h[:])
	return id
}

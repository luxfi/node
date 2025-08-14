// Package consensus provides consensus integration for VMs
package consensus

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

// VerkleIntegration bridges VMs with Verkle+FPC consensus
type VerkleIntegration struct {
	engine *FPCEngine
	db     database.NodeDatabase
	cache  *utils.PointCache
}

// NewVerkleIntegration creates VM-consensus bridge
func NewVerkleIntegration(engine *FPCEngine, db database.NodeDatabase) *VerkleIntegration {
	return &VerkleIntegration{
		engine: engine,
		db:     db,
		cache:  utils.NewPointCache(10000),
	}
}

// ProcessTransactions processes transactions through consensus
func (v *VerkleIntegration) ProcessTransactions(ctx context.Context, txs []Transaction) error {
	// Process each transaction through consensus
	for _, tx := range txs {
		txID := ids.ID(tx.Hash())
		
		// Propose to consensus
		if err := v.engine.Propose(txID); err != nil {
			return err
		}
		
		// Validate Verkle witness if present
		if witness := tx.Witness(); witness != nil {
			header := mockHeader{id: blockIDFromHash(tx.Hash())}
			valid, size, root := v.engine.ValidateWitness(header, witness)
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
		accepted, err := v.engine.Query(txID)
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
func (v *VerkleIntegration) GetVerkleTrie(root common.Hash) (*trie.VerkleTrie, error) {
	return trie.NewVerkleTrie(root, v.db, v.cache)
}

// Transaction represents a transaction with optional witness
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

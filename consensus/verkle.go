// Package consensus provides consensus integration for VMs
package consensus

import (
	"context"

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

		// TODO: Validate Verkle witness when witness package is ready
		// if witness := tx.Witness(); witness != nil {
		//     // Validate witness
		// }
	}

	return nil
}

// GetExecutable returns transactions ready for execution
func (v *VerkleIntegration) GetExecutable() []ids.ID {
	return v.engine.Executable()
}

// Transaction interface for VM transactions
type Transaction interface {
	Hash() [32]byte
	Witness() []byte
}

// Package consensus provides the consensus engine for Lux node
package consensus

import (
	"context"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// FPCEngine provides fast-path consensus for the Lux node
type FPCEngine struct {
	log log.Logger
}

// NewFPCEngine creates a new FPC consensus engine
func NewFPCEngine(f int) *FPCEngine {
	return &FPCEngine{
		log: log.NewLogger("fpc"),
	}
}

// Start initializes the consensus engine
func (e *FPCEngine) Start(ctx context.Context) error {
	e.log.Info("Starting FPC consensus engine")
	return nil
}

// Stop shuts down the consensus engine
func (e *FPCEngine) Stop() error {
	e.log.Info("Stopping FPC consensus engine")
	return nil
}

// Propose adds a transaction to consensus
func (e *FPCEngine) Propose(txID ids.ID) error {
	// TODO: Implement when consensus protocols are ready
	e.log.Debug("Proposed transaction", "txID", txID)
	return nil
}

// Query checks if a transaction is finalized
func (e *FPCEngine) Query(txID ids.ID) (bool, error) {
	// TODO: Implement when consensus protocols are ready
	return false, fmt.Errorf("not finalized: %s", txID)
}

// Executable returns transactions ready for execution
func (e *FPCEngine) Executable() []ids.ID {
	// TODO: Implement when consensus protocols are ready
	return []ids.ID{} // Return empty slice instead of nil
}

// Package consensus provides the consensus engine for Lux node
package consensus

import (
	"context"
	"fmt"
	
	"github.com/luxfi/consensus/flare"
	"github.com/luxfi/consensus/dag"
	"github.com/luxfi/consensus/dag/witness"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// FPCEngine provides fast-path consensus for the Lux node
type FPCEngine struct {
	flare  flare.Flare[ids.ID]
	dag    *dag.DAG
	cache  *witness.Cache
	log    log.Logger
}

// NewFPCEngine creates a new FPC consensus engine
func NewFPCEngine(f int) *FPCEngine {
	// Create Flare fast path
	fl := flare.New[ids.ID](f)
	
	// Create DAG for transaction ordering
	d := dag.New()
	
	// Create witness cache for Verkle validation
	policy := witness.Policy{
		Mode:     witness.Soft,
		MaxBytes: 100 * 1024 * 1024, // 100MB
	}
	wCache := witness.NewCache(policy, 100000, 1<<30)
	
	return &FPCEngine{
		flare: fl,
		dag:   d,
		cache: wCache,
		log:   log.NewLogger("fpc"),
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
	ref := e.flare.Propose(txID)
	e.log.Debug("Proposed transaction", "txID", txID, "ref", ref)
	return nil
}

// Query checks if a transaction is finalized
func (e *FPCEngine) Query(txID ids.ID) (bool, error) {
	status := e.flare.Status(txID)
	if status == flare.StatusFinal || status == flare.StatusExecutable {
		return true, nil
	}
	return false, fmt.Errorf("not finalized: %s", txID)
}

// ValidateWitness validates a Verkle witness
func (e *FPCEngine) ValidateWitness(header witness.Header, payload []byte) (bool, int, [32]byte) {
	return e.cache.Validate(header, payload)
}

// Executable returns transactions ready for execution
func (e *FPCEngine) Executable() []ids.ID {
	return e.flare.Executable()
}

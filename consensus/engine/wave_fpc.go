// Package engine provides consensus engine integration for the Lux node
package engine

import (
	"context"
	"fmt"
	
	"github.com/luxfi/consensus/flare"
	"github.com/luxfi/consensus/dag"
	"github.com/luxfi/consensus/dag/witness"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/snow"
)

// WaveFPCEngine integrates consensus with FPC fast path into the node
type WaveFPCEngine struct {
	flare  flare.Flare[ids.ID]
	dag    *dag.DAG
	cache  *witness.Cache
	ctx    *snow.Context
	log    log.Logger
}

// NewWaveFPCEngine creates a new FPC consensus engine
func NewWaveFPCEngine(ctx *snow.Context, config Config) (*WaveFPCEngine, error) {
	// Create Flare fast path (f=3 for 7 validators)
	f := flare.New[ids.ID](config.F)
	
	// Create DAG for transaction ordering
	d := dag.New()
	
	// Create witness cache for Verkle validation
	policy := witness.Policy{
		Mode:     witness.Soft,
		MaxBytes: 100 * 1024 * 1024, // 100MB
	}
	wCache := witness.NewCache(policy, 100000, 1<<30)
	
	return &WaveFPCEngine{
		flare: f,
		dag:   d,
		cache: wCache,
		ctx:   ctx,
		log:   log.NewLogger("wave-fpc"),
	}, nil
}

// Config for Wave+FPC engine
type Config struct {
	K       int // Sample size
	Alpha   int // Quorum size
	Beta    int // Decision threshold
	Gamma   int // FPC activation threshold
	Delta   int // Confidence delta
	Epsilon int // Termination rounds
	F       int // Byzantine fault tolerance
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		K:       20,
		Alpha:   14,
		Beta:    20,
		Gamma:   5,  // Activate FPC after 5 inconclusive rounds
		Delta:   3,
		Epsilon: 2,
		F:       3,  // Tolerate 3 Byzantine nodes
	}
}

// Start initializes the consensus engine
func (e *WaveFPCEngine) Start(ctx context.Context) error {
	e.log.Info("Starting Wave+FPC consensus engine",
		"fpc", "enabled",
		"verkle", "cached",
	)
	return nil
}

// Stop shuts down the consensus engine
func (e *WaveFPCEngine) Stop() error {
	e.log.Info("Stopping Wave+FPC consensus engine")
	return nil
}

// Query performs a consensus query on a transaction
func (e *WaveFPCEngine) Query(ctx context.Context, txID ids.ID) (bool, error) {
	// Check fast path
	status := e.flare.Status(txID)
	if status == flare.StatusFinal {
		e.log.Debug("Transaction finalized via fast path", "txID", txID)
		return true, nil
	} else if status == flare.StatusExecutable {
		e.log.Debug("Transaction executable via fast path", "txID", txID)
		return true, nil
	}
	
	return false, fmt.Errorf("consensus not reached for %s", txID)
}

// Propose adds a transaction to consensus
func (e *WaveFPCEngine) Propose(ctx context.Context, txID ids.ID) error {
	// Try fast path
	ref := e.flare.Propose(txID)
	e.log.Debug("Proposed transaction", "txID", txID, "ref", ref)
	
	return nil
}

// ValidateWitness validates a Verkle witness using the cache
func (e *WaveFPCEngine) ValidateWitness(header witness.Header, payload []byte) (bool, int, [32]byte) {
	return e.cache.Validate(header, payload)
}

// Metrics returns consensus metrics
func (e *WaveFPCEngine) Metrics() map[string]interface{} {
	return map[string]interface{}{
		"flare_executable": len(e.flare.Executable()),
		"dag_tips":         len(e.dag.GetTips()),
	}
}

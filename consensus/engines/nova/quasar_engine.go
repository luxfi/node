// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package flare

import (
	"context"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/quasar"
)

// QuasarEngine extends the Nova engine with post-quantum finality
type QuasarEngine struct {
	*Engine
	quasar       *quasar.Engine
	novaHook     *quasar.NovaHook
	finalityHook func(context.Context, ids.ID, uint64, []byte) error
}

// NewQuasarEngine creates a Nova engine with Quasar finality
func NewQuasarEngine(params Parameters, quasarConfig quasar.Config) (*QuasarEngine, error) {
	// Create base Nova engine
	nova := NewEngine(params)
	
	// Create Quasar engine
	quasar, err := quasar.NewEngine(quasarConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Quasar engine: %w", err)
	}
	
	// Create integration hook
	novaHook := quasar.NewNovaHook(quasar)
	
	return &QuasarEngine{
		Engine:   nova,
		quasar:   quasar,
		novaHook: novaHook,
	}, nil
}

// Initialize initializes both Nova and Quasar engines
func (e *QuasarEngine) Initialize(ctx context.Context) error {
	// Initialize Quasar
	if err := e.quasar.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize Quasar: %w", err)
	}
	
	// Start integration hook
	e.novaHook.Start(ctx)
	
	// Set finality callback
	e.novaHook.SetFinalizedCallback(func(blockID ids.ID, cert *quasar.DualCertificate) {
		fmt.Printf("Block %s achieved Quasar finality with %d validators\n", 
			blockID, len(cert.SignerIDs))
	})
	
	// Set slashing callback
	e.novaHook.SetSlashingCallback(func(event *quasar.SlashingEvent) {
		fmt.Printf("Slashing event detected: validator %s, type %v\n",
			event.ValidatorID, event.Type)
	})
	
	return nil
}

// SetFinalityHook sets a custom finality hook
func (e *QuasarEngine) SetFinalityHook(hook func(context.Context, ids.ID, uint64, []byte) error) {
	e.finalityHook = hook
}

// finalizeVertex extends the base method to trigger Quasar finality
func (e *QuasarEngine) finalizeVertex(ctx context.Context, vertexID ids.ID, csID ids.ID) error {
	// Call base implementation
	if err := e.Engine.finalizeVertex(ctx, vertexID, csID); err != nil {
		return err
	}
	
	// Get vertex details
	vertex, exists := e.vertices[vertexID]
	if !exists {
		return nil
	}
	
	// For DAG consensus, we need to convert vertex to block representation
	// In production, this would extract the actual block data
	blockHash := vertex.ID().Bytes()
	height := vertex.Height()
	
	// Trigger Quasar finality
	if e.finalityHook != nil {
		if err := e.finalityHook(ctx, vertexID, height, blockHash); err != nil {
			// Log but don't fail Nova consensus
			fmt.Printf("Quasar finality failed for vertex %s: %v\n", vertexID, err)
		}
	} else {
		// Use default Nova hook
		if err := e.novaHook.OnNovaDecided(ctx, vertexID, height, blockHash); err != nil {
			fmt.Printf("Quasar finality failed for vertex %s: %v\n", vertexID, err)
		}
	}
	
	return nil
}

// IsQuasarFinalized checks if a vertex has achieved Quasar finality
func (e *QuasarEngine) IsQuasarFinalized(vertexID ids.ID) bool {
	return e.novaHook.IsFinalized(vertexID)
}

// GetQuasarCertificate returns the Quasar certificate for a finalized vertex
func (e *QuasarEngine) GetQuasarCertificate(vertexID ids.ID) (*quasar.DualCertificate, error) {
	return e.quasar.GetCertificate(vertexID)
}

// GetQuasarMetrics returns Quasar performance metrics
func (e *QuasarEngine) GetQuasarMetrics() quasar.QuasarMetrics {
	return e.quasar.GetMetrics()
}

// Stop stops both engines
func (e *QuasarEngine) Stop() error {
	return e.quasar.Stop()
}
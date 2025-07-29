// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"fmt"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/choices"
)

// NovaHook provides integration between Nova consensus and Quasar finality
type NovaHook struct {
	mu              sync.RWMutex
	quasarEngine    *QuasarEngineWrapper
	enabled         bool
	
	// Track Nova decisions awaiting finality
	pendingDecisions map[ids.ID]*NovaDecision
	
	// Callbacks
	onFinalized     func(ids.ID, *DualCertificate)
	onSlashing      func(*SlashingEvent)
}

// NovaDecision represents a block decided by Nova
type NovaDecision struct {
	BlockID         ids.ID
	Height          uint64
	Hash            []byte
	Timestamp       int64
	Status          choices.Status
}

// NewNovaHook creates a new Nova-Quasar integration hook
func NewNovaHook(quasarEngine *QuasarEngineWrapper) *NovaHook {
	return &NovaHook{
		quasarEngine:     quasarEngine,
		enabled:          true,
		pendingDecisions: make(map[ids.ID]*NovaDecision),
	}
}

// Enable enables Quasar finality for Nova decisions
func (nh *NovaHook) Enable() {
	nh.mu.Lock()
	defer nh.mu.Unlock()
	nh.enabled = true
}

// Disable disables Quasar finality (for testing/rollback)
func (nh *NovaHook) Disable() {
	nh.mu.Lock()
	defer nh.mu.Unlock()
	nh.enabled = false
}

// OnNovaDecided is called when Nova decides on a block
// This is the main integration point
func (nh *NovaHook) OnNovaDecided(ctx context.Context, blockID ids.ID, height uint64, blockHash []byte) error {
	nh.mu.Lock()
	if !nh.enabled {
		nh.mu.Unlock()
		return nil
	}
	
	// Track the decision
	decision := &NovaDecision{
		BlockID:   blockID,
		Height:    height,
		Hash:      blockHash,
		Timestamp: currentTimestamp(),
		Status:    choices.Processing, // Awaiting Quasar finality
	}
	nh.pendingDecisions[blockID] = decision
	nh.mu.Unlock()
	
	// Trigger Quasar finality
	return nh.quasarEngine.OnNovaDecided(ctx, blockID, height, blockHash)
}

// OnQuasarFinalized is called when Quasar achieves finality
func (nh *NovaHook) OnQuasarFinalized(blockID ids.ID, cert *DualCertificate) {
	nh.mu.Lock()
	defer nh.mu.Unlock()
	
	// Update decision status
	if decision, exists := nh.pendingDecisions[blockID]; exists {
		decision.Status = choices.Accepted
		delete(nh.pendingDecisions, blockID)
	}
	
	// Invoke callback
	if nh.onFinalized != nil {
		nh.onFinalized(blockID, cert)
	}
}

// OnSlashingDetected is called when misbehavior is detected
func (nh *NovaHook) OnSlashingDetected(event *SlashingEvent) {
	nh.mu.RLock()
	callback := nh.onSlashing
	nh.mu.RUnlock()
	
	if callback != nil {
		callback(event)
	}
}

// SetFinalizedCallback sets the callback for finalized blocks
func (nh *NovaHook) SetFinalizedCallback(cb func(ids.ID, *DualCertificate)) {
	nh.mu.Lock()
	defer nh.mu.Unlock()
	nh.onFinalized = cb
}

// SetSlashingCallback sets the callback for slashing events
func (nh *NovaHook) SetSlashingCallback(cb func(*SlashingEvent)) {
	nh.mu.Lock()
	defer nh.mu.Unlock()
	nh.onSlashing = cb
}

// Start starts listening to Quasar events
func (nh *NovaHook) Start(ctx context.Context) {
	// Listen for finality events
	go func() {
		finalityCh := nh.quasarEngine.GetFinalityChannel()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-finalityCh:
				nh.OnQuasarFinalized(event.BlockID, event)
			}
		}
	}()
	
	// Listen for slashing events
	go func() {
		slashingCh := nh.quasarEngine.GetSlashingChannel()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-slashingCh:
				nh.OnSlashingDetected(event)
			}
		}
	}()
}

// GetPendingCount returns the number of blocks awaiting finality
func (nh *NovaHook) GetPendingCount() int {
	nh.mu.RLock()
	defer nh.mu.RUnlock()
	return len(nh.pendingDecisions)
}

// IsFinalized checks if a block has achieved Quasar finality
func (nh *NovaHook) IsFinalized(blockID ids.ID) bool {
	return nh.quasarEngine.IsFinalized(blockID)
}

// Integration helper to modify Nova engine

// AttachQuasarToNova attaches Quasar finality to a Nova engine
func AttachQuasarToNova(novaEngine interface{}, quasarEngine *QuasarEngineWrapper) (*NovaHook, error) {
	// Create integration hook
	hook := NewNovaHook(quasarEngine)
	
	// Type assert to get the actual Nova engine
	type NovaEngine interface {
		SetFinalityHook(func(context.Context, ids.ID, uint64, []byte) error)
	}
	
	nova, ok := novaEngine.(NovaEngine)
	if !ok {
		return nil, fmt.Errorf("engine does not support finality hooks")
	}
	
	// Set the hook
	nova.SetFinalityHook(hook.OnNovaDecided)
	
	return hook, nil
}
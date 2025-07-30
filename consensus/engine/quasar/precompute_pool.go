// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/ringtail"
)

var (
	ErrNoPrecompAvailable = errors.New("no precomputed signatures available")
	ErrPrecompExhausted   = errors.New("precomputation pool exhausted")
)

// PrecomputePool manages a pool of precomputed Ringtail signatures
type PrecomputePool struct {
	mu              sync.RWMutex
	pool            []ringtail.Precomp
	targetSize      int
	sk              []byte
	
	// Metrics
	generated       atomic.Uint64
	consumed        atomic.Uint64
	refillsTriggered atomic.Uint64
	
	// Worker control
	stopCh          chan struct{}
	workerWg        sync.WaitGroup
}

// NewPrecomputePool creates a new precomputation pool
func NewPrecomputePool(targetSize int) *PrecomputePool {
	return &PrecomputePool{
		pool:       make([]ringtail.Precomp, 0, targetSize),
		targetSize: targetSize,
		stopCh:     make(chan struct{}),
	}
}

// Initialize starts the precomputation worker with the given secret key
func (p *PrecomputePool) Initialize(sk []byte) error {
	p.mu.Lock()
	p.sk = sk
	p.mu.Unlock()
	
	// Start worker
	p.workerWg.Add(1)
	go p.precomputeWorker()
	
	// Initial fill
	return p.fillPool()
}

// Get retrieves a precomputed signature from the pool
func (p *PrecomputePool) Get() (ringtail.Precomp, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.pool) == 0 {
		return nil, ErrNoPrecompAvailable
	}
	
	// Pop from pool
	precomp := p.pool[len(p.pool)-1]
	p.pool = p.pool[:len(p.pool)-1]
	
	p.consumed.Add(1)
	
	// Trigger refill if below threshold
	if len(p.pool) < p.targetSize/2 {
		select {
		case p.stopCh <- struct{}{}:
			p.refillsTriggered.Add(1)
		default:
		}
	}
	
	return precomp, nil
}

// precomputeWorker continuously generates precomputed signatures
func (p *PrecomputePool) precomputeWorker() {
	defer p.workerWg.Done()
	
	ticker := time.NewTicker(10 * time.Millisecond) // Generate every 10ms
	defer ticker.Stop()
	
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.RLock()
			needMore := len(p.pool) < p.targetSize
			sk := p.sk
			p.mu.RUnlock()
			
			if needMore && sk != nil {
				if precomp, err := ringtail.Precompute(sk); err == nil {
					p.mu.Lock()
					if len(p.pool) < p.targetSize {
						p.pool = append(p.pool, precomp)
						p.generated.Add(1)
					}
					p.mu.Unlock()
				}
			}
		}
	}
}

// fillPool fills the pool to target size
func (p *PrecomputePool) fillPool() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.sk == nil {
		return errors.New("secret key not set")
	}
	
	for len(p.pool) < p.targetSize {
		precomp, err := ringtail.Precompute(p.sk)
		if err != nil {
			return err
		}
		p.pool = append(p.pool, precomp)
		p.generated.Add(1)
	}
	
	return nil
}

// Stop stops the precomputation worker
func (p *PrecomputePool) Stop() {
	close(p.stopCh)
	p.workerWg.Wait()
}

// Size returns the current pool size
func (p *PrecomputePool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pool)
}

// Metrics returns pool statistics
func (p *PrecomputePool) Metrics() map[string]uint64 {
	return map[string]uint64{
		"generated":         p.generated.Load(),
		"consumed":          p.consumed.Load(),
		"refills_triggered": p.refillsTriggered.Load(),
		"current_size":      uint64(p.Size()),
	}
}
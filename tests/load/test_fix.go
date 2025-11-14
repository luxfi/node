// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/ids"
)

// improvedMockIssuer with better failure simulation
type improvedMockIssuer struct {
	tracker      *Tracker[ids.ID]
	failureRate  float64
	delay        time.Duration
	counter      atomic.Uint64
	mu           sync.RWMutex
	issuedTxs    map[ids.ID]time.Time
}

func newImprovedMockIssuer(tracker *Tracker[ids.ID], failureRate float64, delay time.Duration) *improvedMockIssuer {
	return &improvedMockIssuer{
		tracker:     tracker,
		failureRate: failureRate,
		delay:       delay,
		issuedTxs:   make(map[ids.ID]time.Time),
	}
}

func (m *improvedMockIssuer) GenerateAndIssueTx(ctx context.Context) (ids.ID, error) {
	// Simulate processing delay
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ids.Empty, ctx.Err()
		}
	}

	count := m.counter.Add(1)

	// Deterministic failure based on rate
	if m.failureRate > 0 && float64(count%100) < m.failureRate*100 {
		return ids.Empty, ErrFailedToIssueTx
	}

	// Generate unique transaction ID
	id := ids.GenerateTestID()

	m.mu.Lock()
	m.issuedTxs[id] = time.Now()
	m.mu.Unlock()

	return id, nil
}

func (m *improvedMockIssuer) GetIssuedCount() uint64 {
	return m.counter.Load()
}

// improvedMockListener with proper synchronization
type improvedMockListener struct {
	tracker        *Tracker[ids.ID]
	issuedTxs      map[ids.ID]time.Time
	confirmedTxs   map[ids.ID]time.Time
	mu             sync.RWMutex
	delay          time.Duration
	failureRate    float64
	issuingDone    atomic.Bool
	processingWg   sync.WaitGroup
	confirmationWg sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
}

func newImprovedMockListener(tracker *Tracker[ids.ID], failureRate float64, delay time.Duration) *improvedMockListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &improvedMockListener{
		tracker:      tracker,
		issuedTxs:    make(map[ids.ID]time.Time),
		confirmedTxs: make(map[ids.ID]time.Time),
		delay:        delay,
		failureRate:  failureRate,
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (l *improvedMockListener) RegisterIssued(txID ids.ID) {
	l.mu.Lock()
	l.issuedTxs[txID] = time.Now()
	l.mu.Unlock()

	l.processingWg.Add(1)
	l.confirmationWg.Add(1)

	// Start async confirmation processing
	go l.processConfirmation(txID)
}

func (l *improvedMockListener) processConfirmation(id ids.ID) {
	defer l.processingWg.Done()
	defer l.confirmationWg.Done()

	// Simulate confirmation delay
	if l.delay > 0 {
		select {
		case <-time.After(l.delay):
		case <-l.ctx.Done():
			return
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Skip if already confirmed
	if _, confirmed := l.confirmedTxs[id]; confirmed {
		return
	}

	// Simulate failure based on rate
	if l.failureRate > 0 {
		// Use first byte of ID as seed for deterministic failure
		if float64(id[0]%100) < l.failureRate*100 {
			l.tracker.ObserveFailed(id)
			return
		}
	}

	// Confirm the transaction
	l.confirmedTxs[id] = time.Now()
	l.tracker.ObserveConfirmed(id)
}

func (l *improvedMockListener) Listen(ctx context.Context) error {
	// For load testing scenarios, wait for context or all confirmations
	// Create a channel to signal when all confirmations are complete
	doneCh := make(chan struct{})

	// Monitor when all confirmations are complete
	go func() {
		// Wait for issuing to be done
		for !l.issuingDone.Load() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				// Continue checking
			}
		}

		// Then wait for all confirmations to complete
		l.confirmationWg.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		l.cancel() // Cancel any pending confirmations
		// Wait a bit for confirmations to process
		done := make(chan struct{})
		go func() {
			l.processingWg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			// Don't wait too long
		}
		return ctx.Err()
	case <-doneCh:
		return nil
	}
}

func (l *improvedMockListener) IssuingDone() {
	l.issuingDone.Store(true)
}

func (l *improvedMockListener) GetConfirmedCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.confirmedTxs)
}

func (l *improvedMockListener) GetIssuedCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.issuedTxs)
}
// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"
	
	"github.com/luxfi/ids"
)

// scenarioMockIssuer simulates transaction issuance for scenario testing
type scenarioMockIssuer struct {
	tracker     *Tracker[ids.ID]
	counter     atomic.Uint64
	failureRate float64 // Rate of simulated failures (0.0-1.0)
	delay       time.Duration
}

func (m *scenarioMockIssuer) GenerateAndIssueTx(ctx context.Context) (ids.ID, error) {
	// Simulate processing delay
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ids.Empty, ctx.Err()
		}
	}

	count := m.counter.Add(1)

	// Inject failures based on failure rate
	if m.failureRate > 0 && count%uint64(1.0/m.failureRate) == 0 {
		return ids.Empty, errors.New("simulated issuer failure")
	}

	// Generate unique transaction ID
	return ids.GenerateTestID(), nil
}

// scenarioMockListener simulates transaction confirmation listening for scenario testing
type scenarioMockListener struct {
	tracker        *Tracker[ids.ID]
	issuedTxs      map[ids.ID]struct{}
	confirmedTxs   map[ids.ID]struct{}
	mu             sync.RWMutex
	delay          time.Duration
	failureRate    float64
	issuingDone    bool
	processingWg   sync.WaitGroup
	confirmationWg sync.WaitGroup
}

func (l *scenarioMockListener) RegisterIssued(txID ids.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.issuedTxs == nil {
		l.issuedTxs = make(map[ids.ID]struct{})
	}
	if l.confirmedTxs == nil {
		l.confirmedTxs = make(map[ids.ID]struct{})
	}

	l.issuedTxs[txID] = struct{}{}
	l.processingWg.Add(1)
	l.confirmationWg.Add(1)

	// Start async confirmation processing
	go l.processConfirmation(txID)
}

func (l *scenarioMockListener) processConfirmation(id ids.ID) {
	defer l.processingWg.Done()

	// Simulate confirmation delay
	if l.delay > 0 {
		time.Sleep(l.delay)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Skip if already confirmed or failed
	if _, confirmed := l.confirmedTxs[id]; confirmed {
		l.confirmationWg.Done()
		return
	}

	// Simulate failure based on rate
	if l.failureRate > 0 {
		// Use first byte of ID as seed for deterministic failure
		if float64(id[0]%100) < l.failureRate*100 {
			l.tracker.ObserveFailed(id)
			l.confirmationWg.Done()
			return
		}
	}

	// Confirm the transaction
	l.confirmedTxs[id] = struct{}{}
	l.tracker.ObserveConfirmed(id)
	l.confirmationWg.Done()
}

func (l *scenarioMockListener) Listen(ctx context.Context) error {
	// For load testing scenarios, wait for context or all confirmations
	// Confirmations happen in the background via processConfirmation()
	doneCh := make(chan struct{})
	
	// Monitor when all confirmations are complete
	go func() {
		l.confirmationWg.Wait()
		close(doneCh)
	}()
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-doneCh:
		return nil
	}
}

func (l *scenarioMockListener) IssuingDone() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.issuingDone = true
}

// latencyMockListener provides more control over latency distribution
type latencyMockListener struct {
	tracker        *Tracker[ids.ID]
	issuedTxs      []ids.ID
	mu             sync.RWMutex
	minLatency     time.Duration
	maxLatency     time.Duration
	p50Latency     time.Duration
	p95Latency     time.Duration
	p99Latency     time.Duration
	issuingDone    bool
	confirmationWg sync.WaitGroup
}

func (l *latencyMockListener) RegisterIssued(txID ids.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.issuedTxs = append(l.issuedTxs, txID)
	l.confirmationWg.Add(1)

	// Process confirmation with simulated latency
	go l.processWithLatency(txID, len(l.issuedTxs)-1)
}

func (l *latencyMockListener) processWithLatency(id ids.ID, index int) {
	defer l.confirmationWg.Done()

	// Determine latency based on percentile distribution
	var latency time.Duration
	percentile := float64(index%100) / 100.0

	switch {
	case percentile < 0.50:
		// 50th percentile
		latency = l.p50Latency
	case percentile < 0.95:
		// 95th percentile
		latency = l.p95Latency
	case percentile < 0.99:
		// 99th percentile
		latency = l.p99Latency
	default:
		// Max latency for outliers
		latency = l.maxLatency
	}

	// Add some jitter
	if latency > 0 {
		jitter := time.Duration(float64(latency) * 0.1 * (float64(index%10) - 5) / 5)
		latency += jitter
		if latency < l.minLatency {
			latency = l.minLatency
		}
	}

	time.Sleep(latency)
	l.tracker.ObserveConfirmed(id)
}

func (l *latencyMockListener) Listen(ctx context.Context) error {
	// For load testing scenarios, wait for context or all confirmations
	// Confirmations happen in the background via processWithLatency()
	doneCh := make(chan struct{})
	
	// Monitor when all confirmations are complete
	go func() {
		l.confirmationWg.Wait()
		close(doneCh)
	}()
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-doneCh:
		return nil
	}
}

func (l *latencyMockListener) IssuingDone() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.issuingDone = true
}

// burstIssuer issues transactions in bursts for spike testing
type burstIssuer struct {
	tracker     *Tracker[ids.ID]
	burstSize   int
	burstDelay  time.Duration
	counter     atomic.Uint64
}

func (b *burstIssuer) GenerateAndIssueTx(ctx context.Context) (ids.ID, error) {
	count := b.counter.Add(1)

	// Add delay between bursts
	if b.burstSize > 0 && int(count)%b.burstSize == 0 && b.burstDelay > 0 {
		select {
		case <-time.After(b.burstDelay):
		case <-ctx.Done():
			return ids.Empty, ctx.Err()
		}
	}

	return ids.GenerateTestID(), nil
}

// errorIssuer simulates various error scenarios
type errorIssuer struct {
	tracker       *Tracker[ids.ID]
	errorRate     float64
	errorPattern  string // "random", "burst", "degrading"
	counter       atomic.Uint64
	degradeStart  uint64
	degradeWindow uint64
}

func (e *errorIssuer) GenerateAndIssueTx(ctx context.Context) (ids.ID, error) {
	count := e.counter.Add(1)

	var shouldError bool
	switch e.errorPattern {
	case "burst":
		// Error in bursts of 10
		shouldError = (count/10)%2 == 0 && e.errorRate > 0
	case "degrading":
		// Gradually increase error rate
		if count > e.degradeStart {
			progress := float64(count-e.degradeStart) / float64(e.degradeWindow)
			currentRate := math.Min(e.errorRate*progress, 1.0)
			shouldError = float64(count%100) < currentRate*100
		}
	default: // "random"
		shouldError = e.errorRate > 0 && float64(count%100) < e.errorRate*100
	}

	if shouldError {
		return ids.Empty, errors.New("simulated error")
	}

	return ids.GenerateTestID(), nil
}
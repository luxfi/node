// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests"
)

// TestScenarioSustainedLoad tests a constant TPS over a duration
func TestScenarioSustainedLoad(t *testing.T) {
	tests := []struct {
		name          string
		targetTPS     int64
		duration      time.Duration
		expectedTxMin uint64 // minimum expected transactions
	}{
		{
			name:          "100 TPS for 10 seconds",
			targetTPS:     100,
			duration:      10 * time.Second,
			expectedTxMin: 900, // Allow 10% variance
		},
		{
			name:          "500 TPS for 5 seconds",
			targetTPS:     500,
			duration:      5 * time.Second,
			expectedTxMin: 2250, // Allow 10% variance
		},
		{
			name:          "1000 TPS for 3 seconds",
			targetTPS:     1000,
			duration:      3 * time.Second,
			expectedTxMin: 2700, // Allow 10% variance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			log := tests.NewDefaultLogger("")
			registry := metric.NewRegistry()
			metrics, err := NewMetrics(registry)
			require.NoError(err)

			tracker := NewTracker[txID](metrics)

			// Create 5 agents
			numAgents := 5
			agents := make([]Agent[txID], numAgents)
			for i := range agents {
				agents[i] = Agent[txID]{
					Issuer:   &mockIssuer{tracker: tracker},
					Listener: &mockListener{tracker: tracker, delay: 100 * time.Millisecond},
				}
			}

			config := OrchestratorConfig{
				MaxTPS:           tt.targetTPS,
				MinTPS:           tt.targetTPS, // Start at target TPS
				Step:             0,             // No stepping
				TxRateMultiplier: 1.0,          // Exact rate
				SustainedTime:    2 * time.Second,
				MaxAttempts:      1,
				Terminate:        false, // Run for duration
			}

			orchestrator := NewOrchestrator(agents, tracker, log, config)

			// Run for specified duration
			ctx, cancel := context.WithTimeout(ctx, tt.duration)
			defer cancel()

			err = orchestrator.Execute(ctx)
			require.ErrorIs(err, context.DeadlineExceeded)

			// Verify minimum transactions achieved
			confirmed := tracker.GetObservedConfirmed()
			require.GreaterOrEqual(confirmed, tt.expectedTxMin,
				"Expected at least %d confirmed transactions, got %d", tt.expectedTxMin, confirmed)

			// Calculate actual TPS
			actualTPS := float64(confirmed) / tt.duration.Seconds()
			expectedTPS := float64(tt.targetTPS)

			// Allow 15% variance (due to timing and coordination overhead)
			require.InDelta(expectedTPS, actualTPS, expectedTPS*0.15,
				"TPS variance too high: expected %.1f, got %.1f", expectedTPS, actualTPS)
		})
	}
}

// TestScenarioRampUp tests gradually increasing TPS
func TestScenarioRampUp(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := tests.NewDefaultLogger("")
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[txID](metrics)

	// Create 5 agents
	numAgents := 5
	agents := make([]Agent[txID], numAgents)
	for i := range agents {
		agents[i] = Agent[txID]{
			Issuer:   &mockIssuer{tracker: tracker},
			Listener: &mockListener{tracker: tracker, delay: 100 * time.Millisecond},
		}
	}

	config := OrchestratorConfig{
		MaxTPS:           1000,
		MinTPS:           100,
		Step:             100,
		TxRateMultiplier: 1.1,
		SustainedTime:    2 * time.Second,
		MaxAttempts:      3,
		Terminate:        true,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	err = orchestrator.Execute(ctx)
	require.NoError(err)

	// Verify we ramped up successfully
	maxObservedTPS := orchestrator.GetMaxObservedTPS()
	require.GreaterOrEqual(maxObservedTPS, int64(500),
		"Expected to ramp up to at least 500 TPS, got %d", maxObservedTPS)

	// Verify we issued and confirmed transactions
	issued := tracker.GetObservedIssued()
	confirmed := tracker.GetObservedConfirmed()
	require.Greater(issued, uint64(0), "No transactions issued")
	require.Greater(confirmed, uint64(0), "No transactions confirmed")

	// Success rate should be high
	successRate := float64(confirmed) / float64(issued)
	require.Greater(successRate, 0.85, "Success rate too low: %.2f", successRate)
}

// TestScenarioSpike tests sudden TPS burst
func TestScenarioSpike(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := tests.NewDefaultLogger("")
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[txID](metrics)

	// Create 10 agents for higher throughput
	numAgents := 10
	agents := make([]Agent[txID], numAgents)
	for i := range agents {
		agents[i] = Agent[txID]{
			Issuer:   &mockIssuer{tracker: tracker},
			Listener: &mockListener{tracker: tracker, delay: 50 * time.Millisecond},
		}
	}

	// Burst mode: MaxTPS = -1
	config := OrchestratorConfig{
		MaxTPS:           -1,
		MinTPS:           5000, // Burst 5000 transactions
		Step:             0,
		TxRateMultiplier: 1.0,
		SustainedTime:    5 * time.Second,
		MaxAttempts:      1,
		Terminate:        true,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	err = orchestrator.Execute(ctx)
	require.NoError(err)

	// Verify burst completed
	issued := tracker.GetObservedIssued()
	confirmed := tracker.GetObservedConfirmed()

	require.Equal(uint64(5000), issued, "Expected exactly 5000 transactions issued")
	require.Equal(uint64(5000), confirmed, "Expected all 5000 transactions confirmed")
}

// TestScenarioSoak tests long-duration stability
func TestScenarioSoak(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping soak test in short mode")
	}

	require := require.New(t)
	ctx := context.Background()

	log := tests.NewDefaultLogger("")
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[txID](metrics)

	// Create 3 agents
	numAgents := 3
	agents := make([]Agent[txID], numAgents)
	for i := range agents {
		agents[i] = Agent[txID]{
			Issuer:   &mockIssuer{tracker: tracker},
			Listener: &mockListener{tracker: tracker, delay: 100 * time.Millisecond},
		}
	}

	config := OrchestratorConfig{
		MaxTPS:           300,
		MinTPS:           300,
		Step:             0,
		TxRateMultiplier: 1.0,
		SustainedTime:    5 * time.Second,
		MaxAttempts:      1,
		Terminate:        false,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	// Run for 30 seconds (soak test)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = orchestrator.Execute(ctx)
	require.ErrorIs(err, context.DeadlineExceeded)

	// Verify sustained performance over time
	confirmed := tracker.GetObservedConfirmed()
	failed := tracker.GetObservedFailed()

	// At 300 TPS for 30 seconds, expect ~9000 transactions (allow 10% variance)
	require.GreaterOrEqual(confirmed, uint64(8100), "Expected at least 8100 confirmed transactions")

	// Failure rate should be very low
	if confirmed > 0 {
		failureRate := float64(failed) / float64(confirmed+failed)
		require.Less(failureRate, 0.01, "Failure rate too high for soak test: %.2f", failureRate)
	}
}

// TestScenarioStressRecovery tests recovery from temporary failures
func TestScenarioStressRecovery(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := tests.NewDefaultLogger("")
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[txID](metrics)

	// Create agents with intermittent failures
	numAgents := 5
	agents := make([]Agent[txID], numAgents)
	for i := range agents {
		agents[i] = Agent[txID]{
			Issuer: &mockIssuer{
				tracker:     tracker,
				failureRate: 0.1, // 10% failure rate
			},
			Listener: &mockListener{
				tracker: tracker,
				delay:   150 * time.Millisecond,
			},
		}
	}

	config := OrchestratorConfig{
		MaxTPS:           500,
		MinTPS:           100,
		Step:             100,
		TxRateMultiplier: 1.2,
		SustainedTime:    3 * time.Second,
		MaxAttempts:      5, // Allow retries
		Terminate:        true,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	err = orchestrator.Execute(ctx)
	// May fail to reach target due to injected failures, but should make progress
	if err != nil {
		require.ErrorIs(err, ErrFailedToReachTargetTPS)
	}

	// Verify system recovered and made progress despite failures
	confirmed := tracker.GetObservedConfirmed()
	failed := tracker.GetObservedFailed()

	require.Greater(confirmed, uint64(100), "Should have confirmed some transactions")

	// Calculate observed failure rate
	totalProcessed := confirmed + failed
	if totalProcessed > 0 {
		observedFailureRate := float64(failed) / float64(totalProcessed)
		// Should be close to injected 10% rate (allow variance)
		require.InDelta(0.1, observedFailureRate, 0.05,
			"Observed failure rate %.2f differs from expected 0.10", observedFailureRate)
	}
}

// mockIssuer with optional failure injection
type mockIssuerFailing struct {
	tracker     *Tracker[txID]
	failureRate float64
	counter     uint64
}

func (m *mockIssuerFailing) GenerateAndIssueTx(ctx context.Context) (txID, error) {
	m.counter++

	// Inject failures based on failure rate
	if m.failureRate > 0 {
		threshold := uint64(math.Round(1.0 / m.failureRate))
		if m.counter%threshold == 0 {
			return txID{}, errors.New("injected failure")
		}
	}

	id := txID{}
	for i := range id {
		id[i] = byte(m.counter + uint64(i))
	}
	return id, nil
}

// TestMetricsCollection verifies metrics are collected correctly
func TestMetricsCollection(t *testing.T) {
	require := require.New(t)

	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[txID](metrics)

	// Issue some transactions
	for i := 0; i < 100; i++ {
		id := txID{}
		id[0] = byte(i)
		tracker.Issue(id)
	}

	require.Equal(uint64(100), tracker.GetObservedIssued())

	// Simulate confirmations with varying latency
	for i := 0; i < 80; i++ {
		id := txID{}
		id[0] = byte(i)

		// Simulate different latencies
		time.Sleep(time.Microsecond * time.Duration(i%10))
		tracker.ObserveConfirmed(id)
	}

	require.Equal(uint64(80), tracker.GetObservedConfirmed())

	// Simulate some failures
	for i := 80; i < 90; i++ {
		id := txID{}
		id[0] = byte(i)
		tracker.ObserveFailed(id)
	}

	require.Equal(uint64(10), tracker.GetObservedFailed())

	// Verify success rate calculation
	confirmed := tracker.GetObservedConfirmed()
	failed := tracker.GetObservedFailed()
	successRate := float64(confirmed) / float64(confirmed+failed)
	require.InDelta(0.888, successRate, 0.01) // 80/90 = 0.888...
}

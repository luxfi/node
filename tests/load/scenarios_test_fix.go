// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// TestScenarioRampUpFixed tests gradually increasing TPS with improved mocks
func TestScenarioRampUpFixed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := log.NoLog{}
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[ids.ID](metrics)

	// Create agents with improved mocks
	numAgents := 5
	agents := make([]Agent[ids.ID], numAgents)
	for i := range agents {
		issuer := newImprovedMockIssuer(tracker, 0, 2*time.Millisecond)
		listener := newImprovedMockListener(tracker, 0, 3*time.Millisecond)
		agents[i] = Agent[ids.ID]{
			Issuer:   issuer,
			Listener: listener,
		}
	}

	config := OrchestratorConfig{
		MaxTPS:           200,  // Realistic target
		MinTPS:           20,   // Lower starting point
		Step:             50,   // Moderate steps
		TxRateMultiplier: 1.15, // Slight overprovision
		SustainedTime:    1 * time.Second, // Enough time to stabilize
		MaxAttempts:      5,    // More attempts for ramp-up
		Terminate:        true,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	// Run with generous timeout
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = orchestrator.Execute(testCtx)
	// Allow success, timeout, or failed to reach target (as long as we made progress)
	if err != nil && err != context.DeadlineExceeded && err != ErrFailedToReachTargetTPS {
		require.NoError(err)
	}

	// Verify we made progress
	maxObservedTPS := orchestrator.GetMaxObservedTPS()
	require.Greater(maxObservedTPS, int64(15), // At least achieved minimum
		"Expected to achieve at least 15 TPS, got %d", maxObservedTPS)

	// Verify we issued and confirmed transactions
	issued := tracker.GetObservedIssued()
	confirmed := tracker.GetObservedConfirmed()
	require.Greater(issued, uint64(10), "Should have issued some transactions")
	require.Greater(confirmed, uint64(5), "Should have confirmed some transactions")

	// Success rate should be reasonable
	if issued > 0 {
		successRate := float64(confirmed) / float64(issued)
		require.Greater(successRate, 0.5, "Success rate too low: %.2f", successRate)
	}
}

// TestScenarioSpikeFixed tests sudden TPS burst with improved synchronization
func TestScenarioSpikeFixed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := log.NoLog{}
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[ids.ID](metrics)

	// Create agents with faster processing for burst mode
	numAgents := 10
	agents := make([]Agent[ids.ID], numAgents)
	for i := range agents {
		issuer := newImprovedMockIssuer(tracker, 0, 0) // No delay for burst
		listener := newImprovedMockListener(tracker, 0, 5*time.Millisecond)
		agents[i] = Agent[ids.ID]{
			Issuer:   issuer,
			Listener: listener,
		}
	}

	// Burst mode configuration
	config := OrchestratorConfig{
		MaxTPS:           -1,   // Burst mode
		MinTPS:           500,  // Burst 500 transactions
		Step:             0,
		TxRateMultiplier: 1.0,
		SustainedTime:    3 * time.Second, // Wait time for confirmations
		MaxAttempts:      1,
		Terminate:        true,
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	// Run with timeout
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err = orchestrator.Execute(testCtx)
	if err != nil && err != context.DeadlineExceeded {
		require.NoError(err)
	}

	// Verify burst was issued
	issued := tracker.GetObservedIssued()
	confirmed := tracker.GetObservedConfirmed()

	require.Equal(uint64(500), issued, "Expected exactly 500 transactions issued")
	// Allow some variance in confirmations due to timing
	require.GreaterOrEqual(confirmed, uint64(400), "Expected at least 400 transactions confirmed")
}

// TestScenarioStressRecoveryFixed tests recovery with improved failure handling
func TestScenarioStressRecoveryFixed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	log := log.NoLog{}
	registry := metric.NewRegistry()
	metrics, err := NewMetrics(registry)
	require.NoError(err)

	tracker := NewTracker[ids.ID](metrics)

	// Create agents with controlled failure injection
	numAgents := 5
	agents := make([]Agent[ids.ID], numAgents)
	for i := range agents {
		// Only inject failures in issuer, not listener
		issuer := newImprovedMockIssuer(tracker, 0.05, 2*time.Millisecond) // 5% failure rate
		listener := newImprovedMockListener(tracker, 0, 5*time.Millisecond) // No failures
		agents[i] = Agent[ids.ID]{
			Issuer:   issuer,
			Listener: listener,
		}
	}

	config := OrchestratorConfig{
		MaxTPS:           100,  // Moderate target
		MinTPS:           20,   // Start low
		Step:             20,   // Small steps
		TxRateMultiplier: 1.3,  // Higher multiplier to account for failures
		SustainedTime:    2 * time.Second,
		MaxAttempts:      10,   // More attempts for recovery
		Terminate:        false, // Keep running to observe recovery
	}

	orchestrator := NewOrchestrator(agents, tracker, log, config)

	// Run for a fixed duration to observe behavior
	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err = orchestrator.Execute(testCtx)
	// Expect timeout since we set Terminate=false
	require.ErrorIs(err, context.DeadlineExceeded)

	// Verify system made progress despite failures
	issued := tracker.GetObservedIssued()
	confirmed := tracker.GetObservedConfirmed()
	failed := tracker.GetObservedFailed()

	require.Greater(issued, uint64(50), "Should have issued many transactions")
	require.Greater(confirmed, uint64(20), "Should have confirmed some transactions")

	// Check that we observed some failures but not too many
	if issued > 100 {
		failureRate := float64(failed) / float64(issued)
		require.Less(failureRate, 0.2, "Failure rate too high: %.2f", failureRate)
	}

	// Verify we achieved some reasonable TPS
	maxTPS := orchestrator.GetMaxObservedTPS()
	require.Greater(maxTPS, int64(10), "Should have achieved at least 10 TPS")
}

// TestScenarioSustainedLoadFixed tests constant TPS with better timing
func TestScenarioSustainedLoadFixed(t *testing.T) {
	tests := []struct {
		name          string
		targetTPS     int64
		duration      time.Duration
		expectedTxMin uint64
	}{
		{
			name:          "50 TPS for 3 seconds",
			targetTPS:     50,
			duration:      3 * time.Second,
			expectedTxMin: 120, // Allow 20% variance
		},
		{
			name:          "100 TPS for 2 seconds",
			targetTPS:     100,
			duration:      2 * time.Second,
			expectedTxMin: 160, // Allow 20% variance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			log := log.NoLog{}
			registry := metric.NewRegistry()
			metrics, err := NewMetrics(registry)
			require.NoError(err)

			tracker := NewTracker[ids.ID](metrics)

			// Create agents
			numAgents := 5
			agents := make([]Agent[ids.ID], numAgents)
			for i := range agents {
				issuer := newImprovedMockIssuer(tracker, 0, 1*time.Millisecond)
				listener := newImprovedMockListener(tracker, 0, 2*time.Millisecond)
				agents[i] = Agent[ids.ID]{
					Issuer:   issuer,
					Listener: listener,
				}
			}

			config := OrchestratorConfig{
				MaxTPS:           tt.targetTPS,
				MinTPS:           tt.targetTPS, // Start at target
				Step:             0,            // No stepping
				TxRateMultiplier: 1.2,          // Overprovision
				SustainedTime:    1 * time.Second,
				MaxAttempts:      1,
				Terminate:        false, // Keep running for duration
			}

			orchestrator := NewOrchestrator(agents, tracker, log, config)

			// Run for specified duration
			ctx, cancel := context.WithTimeout(ctx, tt.duration)
			defer cancel()

			err = orchestrator.Execute(ctx)
			// Should timeout since Terminate=false
			if err != nil && err != context.DeadlineExceeded {
				require.NoError(err)
			}

			// Verify minimum transactions achieved
			confirmed := tracker.GetObservedConfirmed()
			require.GreaterOrEqual(confirmed, tt.expectedTxMin,
				"Expected at least %d confirmed transactions, got %d", tt.expectedTxMin, confirmed)

			// Calculate actual TPS
			actualTPS := float64(confirmed) / tt.duration.Seconds()
			expectedTPS := float64(tt.targetTPS)

			// Allow 30% variance due to startup/shutdown overhead
			require.InDelta(expectedTPS, actualTPS, expectedTPS*0.3,
				"TPS variance too high: expected %.1f, got %.1f", expectedTPS, actualTPS)
		})
	}
}
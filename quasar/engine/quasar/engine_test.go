// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestQuasarEngine tests the main Quasar engine functionality
func TestQuasarEngine(t *testing.T) {
	require := require.New(t)
	
	// Create Quasar engine
	params := Parameters{
		K:                     5,
		AlphaPreference:       3,
		AlphaConfidence:       4,
		Beta:                  3,
		MaxItemProcessingTime: 5 * time.Second,
	}
	nodeID := NodeID("test-node-1")
	
	engine := NewEngine(params, nodeID)
	require.NotNil(engine)
	
	// Initialize engine
	ctx := context.Background()
	err := engine.Initialize(ctx)
	require.NoError(err)
	
	// Test consensus status
	status := engine.ConsensusStatus()
	require.NotNil(status)
	
	// Verify status contains expected fields
	statusMap, ok := status.(map[string]interface{})
	require.True(ok)
	require.Equal(nodeID, statusMap["nodeID"])
	require.Equal(params, statusMap["params"])
}

// TestEngineInitialization tests engine initialization
func TestEngineInitialization(t *testing.T) {
	require := require.New(t)
	
	// Test with different parameters
	testCases := []struct {
		name   string
		params Parameters
	}{
		{
			name: "default parameters",
			params: DefaultParameters,
		},
		{
			name: "custom parameters",
			params: Parameters{
				K:                     10,
				AlphaPreference:       6,
				AlphaConfidence:       8,
				Beta:                  5,
				MaxItemProcessingTime: 10 * time.Second,
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := NodeID(fmt.Sprintf("test-node-%s", tc.name))
			engine := NewEngine(tc.params, nodeID)
			require.NotNil(engine)
			
			// Verify engine fields
			require.Equal(tc.params, engine.params)
			require.Equal(nodeID, engine.nodeID)
			
			// Initialize engine
			ctx := context.Background()
			err := engine.Initialize(ctx)
			require.NoError(err)
		})
	}
}

// TestParameterValidation tests parameter validation
func TestParameterValidation(t *testing.T) {
	require := require.New(t)
	
	// Test various parameter combinations
	testCases := []struct {
		name        string
		params      Parameters
		expectValid bool
	}{
		{
			name: "valid parameters",
			params: Parameters{
				K:                     5,
				AlphaPreference:       3,
				AlphaConfidence:       4,
				Beta:                  2,
				MaxItemProcessingTime: 5 * time.Second,
			},
			expectValid: true,
		},
		{
			name: "alpha preference > K/2",
			params: Parameters{
				K:                     5,
				AlphaPreference:       3, // > 5/2
				AlphaConfidence:       4,
				Beta:                  2,
				MaxItemProcessingTime: 5 * time.Second,
			},
			expectValid: true,
		},
		{
			name: "alpha confidence >= alpha preference",
			params: Parameters{
				K:                     5,
				AlphaPreference:       3,
				AlphaConfidence:       4, // >= 3
				Beta:                  2,
				MaxItemProcessingTime: 5 * time.Second,
			},
			expectValid: true,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := NodeID("test-node")
			engine := NewEngine(tc.params, nodeID)
			
			if tc.expectValid {
				require.NotNil(engine)
				require.Equal(tc.params, engine.params)
			} else {
				// For invalid parameters, we might want to add validation
				// in the future
				require.NotNil(engine)
			}
		})
	}
}

// TestConcurrentAccess tests concurrent access to engine
func TestConcurrentAccess(t *testing.T) {
	require := require.New(t)
	
	params := Parameters{
		K:                     5,
		AlphaPreference:       3,
		AlphaConfidence:       4,
		Beta:                  3,
		MaxItemProcessingTime: 5 * time.Second,
	}
	nodeID := NodeID("test-concurrent")
	engine := NewEngine(params, nodeID)
	require.NotNil(engine)
	
	// Initialize engine
	ctx := context.Background()
	err := engine.Initialize(ctx)
	require.NoError(err)
	
	// Test concurrent access to ConsensusStatus
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			status := engine.ConsensusStatus()
			require.NotNil(status)
			done <- true
		}()
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestMultipleEngines tests multiple engines running
func TestMultipleEngines(t *testing.T) {
	require := require.New(t)
	
	params := Parameters{
		K:                     5,
		AlphaPreference:       3,
		AlphaConfidence:       4,
		Beta:                  3,
		MaxItemProcessingTime: 5 * time.Second,
	}
	
	// Create multiple engines
	engines := make([]*Engine, 3)
	for i := 0; i < 3; i++ {
		nodeID := NodeID(fmt.Sprintf("node-%d", i))
		engines[i] = NewEngine(params, nodeID)
		require.NotNil(engines[i])
		
		// Initialize each engine
		ctx := context.Background()
		err := engines[i].Initialize(ctx)
		require.NoError(err)
	}
	
	// Verify each engine has unique node ID
	for i := 0; i < 3; i++ {
		for j := i + 1; j < 3; j++ {
			require.NotEqual(engines[i].nodeID, engines[j].nodeID)
		}
	}
}

// Helper functions are defined in helpers_test.go
package sampling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestK1Parameters tests that k=1 consensus parameters are valid
func TestK1Parameters(t *testing.T) {
	// Valid k=1 parameters
	k1Params := Parameters{
		K:                     1,
		AlphaPreference:       1, // Must be > K/2 = 0.5, so 1 is valid
		AlphaConfidence:       1, // Must be >= AlphaPreference and <= K
		Beta:                  1, // Number of consecutive successful queries
		ConcurrentRepolls:     1, // Must be > 0 and <= Beta
		OptimalProcessing:     1, // Must be > 0
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	// Verify k=1 parameters are valid
	require.NoError(t, k1Params.Verify())
}

// TestK1ParametersWithHigherBeta tests k=1 with higher beta for more confidence
func TestK1ParametersWithHigherBeta(t *testing.T) {
	// k=1 with higher beta (more rounds needed for finalization)
	k1ParamsHighBeta := Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  20, // Require 20 consecutive successful polls
		ConcurrentRepolls:     4,  // Can have up to 4 concurrent polls
		OptimalProcessing:     10,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	require.NoError(t, k1ParamsHighBeta.Verify())
}

// TestInvalidK1Parameters tests invalid k=1 configurations
func TestInvalidK1Parameters(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		errMsg string
	}{
		{
			name: "AlphaPreference too low",
			params: Parameters{
				K:                     1,
				AlphaPreference:       0, // Invalid: must be > K/2 = 0.5
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1 * time.Second,
			},
			errMsg: "k/2 < alphaPreference",
		},
		{
			name: "AlphaConfidence too high",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       2, // Invalid: must be <= K
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1 * time.Second,
			},
			errMsg: "alphaConfidence <= k",
		},
		{
			name: "ConcurrentRepolls too high",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     2, // Invalid: must be <= Beta
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1 * time.Second,
			},
			errMsg: "concurrentRepolls <= beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Verify()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestMinPercentConnectedHealthyK1 tests the minimum connectivity for k=1
func TestMinPercentConnectedHealthyK1(t *testing.T) {
	k1Params := Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	// For k=1, alphaConfidence=1, the ratio is 1/1 = 1.0
	// MinPercentConnected = 1.0 * (1 - 0.2) + 0.2 = 0.8 + 0.2 = 1.0
	expected := 1.0
	actual := k1Params.MinPercentConnectedHealthy()
	require.Equal(t, expected, actual)
}

// TestRealisticK1Parameters tests realistic k=1 configurations for mainnet
func TestRealisticK1Parameters(t *testing.T) {
	// Realistic k=1 parameters for single-node mainnet
	mainnetK1 := Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  20, // Higher beta for production confidence
		ConcurrentRepolls:     4,
		OptimalProcessing:     10,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	require.NoError(t, mainnetK1.Verify())
	
	// Test that we need 100% connectivity (single node must be connected to itself)
	require.Equal(t, 1.0, mainnetK1.MinPercentConnectedHealthy())
}
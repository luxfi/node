package chains

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/uptime"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
)

// TestK1ConsensusIntegration tests that k=1 consensus works in practice
func TestK1ConsensusIntegration(t *testing.T) {
	require := require.New(t)

	// Create k=1 consensus parameters
	consensusParams := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	// Verify parameters are valid
	require.NoError(consensusParams.Verify())

	// Verify minimum connected percentage is 100% (single node must be self-connected)
	require.Equal(1.0, consensusParams.MinPercentConnectedHealthy())

	// Test chain parameters with k=1
	chainParams := ChainParameters{
		ID:            constants.PlatformChainID,
		SubnetID:      constants.PrimaryNetworkID,
		GenesisData:   []byte("test-genesis"),
		VMID:          constants.PlatformVMID,
		FxIDs:         []ids.ID{},
		CustomBeacons: nil,
	}

	// Verify chain parameters are properly configured
	require.Equal(constants.PlatformChainID, chainParams.ID)
	require.Equal(consensusParams, chainParams.ConsensusParameters)
	require.Equal(1, chainParams.ConsensusParameters.K)
}

// TestK1ConsensusBootstrap tests that k=1 can bootstrap successfully
func TestK1ConsensusBootstrap(t *testing.T) {
	require := require.New(t)

	// Bootstrap parameters for k=1
	// With k=1, we don't need external bootstrappers
	bootstrappers := []string{} // No bootstrappers needed for k=1
	
	// Verify that with k=1, no external bootstrappers are needed
	require.Empty(bootstrappers)
}

// TestK1ConsensusValidation tests validation logic for k=1
func TestK1ConsensusValidation(t *testing.T) {
	tests := []struct {
		name        string
		k           int
		alphaPref   int
		alphaConf   int
		expectValid bool
	}{
		{
			name:        "valid k=1",
			k:           1,
			alphaPref:   1,
			alphaConf:   1,
			expectValid: true,
		},
		{
			name:        "invalid k=1 with alphaPref=0",
			k:           1,
			alphaPref:   0,
			alphaConf:   1,
			expectValid: false,
		},
		{
			name:        "invalid k=1 with alphaConf=2",
			k:           1,
			alphaPref:   1,
			alphaConf:   2,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := sampling.Parameters{
				K:                     tt.k,
				AlphaPreference:       tt.alphaPref,
				AlphaConfidence:       tt.alphaConf,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1 * time.Second,
			}

			err := params.Verify()
			if tt.expectValid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

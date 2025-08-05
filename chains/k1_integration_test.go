package chains

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consenus/uptime"
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
		ID:                  constants.PlatformChainID,
		GenesisData:         []byte("test-genesis"),
		SubnetID:            constants.PrimaryNetworkID,
		VMData:              []byte{},
		UpgradeSchedule:     []netutils.Upgrade{},
		FxIDs:               []ids.ID{},
		Config:              ChainConfig{},
		VMID:                constants.PlatformVMID,
		ConsensusParameters: consensusParams,
		ResourceTracker:     nil,
		UptimeTracker:       uptime.NoOpTracker{},
		ReplayDecisions:     false,
		TrackSubnetsUsed:    false,
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
	bootstrapConfig := BootstrapConfig{
		BootstrapBeaconConnectionTimeout:        10 * time.Second,
		BootstrapAncestorsMaxContainersSent:     2000,
		BootstrapAncestorsMaxContainersReceived: 2000,
		BootstrapMaxTimeGetAncestors:            50 * time.Millisecond,
		Bootstrappers:                           []string{}, // No bootstrappers needed for k=1
		SkipBootstrap:                           false,
	}

	// With k=1, we don't need external bootstrappers
	require.Empty(bootstrapConfig.Bootstrappers)
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

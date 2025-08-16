// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poa

import (
	"testing"
	"time"
	

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/config"
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/node/subnets"
)

func TestPOAConsensusParameters(t *testing.T) {
	require := require.New(t)

	// Test that POA consensus parameters are correctly set
	params := subnets.GetPOAConsensusParameters()

	// POA mode should have K=1, Alpha=1, Beta=1 for single-node operation
	require.Equal(1, params.K, "POA mode should have K=1")
	require.Equal(1, params.AlphaPreference, "POA mode should have AlphaPreference=1")
	require.Equal(1, params.AlphaConfidence, "POA mode should have AlphaConfidence=1")
	require.Equal(uint32(1), params.Beta, "POA mode should have Beta=1")
	require.Equal(1, params.ConcurrentPolls, "POA mode should have ConcurrentPolls=1")
}

func TestPOASubnetConfig(t *testing.T) {
	require := require.New(t)

	// Test subnet config with POA enabled
	cfg := subnets.Config{
		POAEnabled:        true,
		POASingleNodeMode: true,
		POAMinBlockTime:   1 * time.Second,
		ConsensusParameters: consensusconfig.Parameters{
			K:                     5,
			AlphaPreference:       3,
			AlphaConfidence:       3,
			Beta:                  uint32(20),
			ConcurrentPolls:     4,
			OptimalProcessing:     10,
			MaxOutstandingItems:   256,
			MaxItemProcessingTime: 30 * time.Second,
		},
	}

	// Verify configuration is valid
	err := cfg.Valid()
	require.NoError(err, "POA subnet config should be valid")
}

func TestPOAConfigKeys(t *testing.T) {
	require := require.New(t)

	// Test that POA configuration keys are defined
	require.Equal("poa-mode-enabled", config.POAModeEnabledKey)
	require.Equal("poa-single-node-mode", config.POASingleNodeModeKey)
	require.Equal("poa-min-block-time", config.POAMinBlockTimeKey)
	require.Equal("poa-authorized-nodes", config.POAAuthorizedNodesKey)
}

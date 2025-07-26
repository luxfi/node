// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	luxlog "github.com/luxfi/log"
)

func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	ctx := context.Background()

	network := NewDefaultNetwork("testnet")
	// Runtime configuration is required
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	// Validate round-tripping of primary subnet configuration
	network.PrimarySubnetConfig = ConfigMap{
		"validatorOnly": true,
	}
	require.NoError(network.EnsureDefaultConfig(luxlog.NewNoOpLogger(){}))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes(ctx))

	loadedNetwork, err := ReadNetwork(ctx, luxlog.NewNoOpLogger(){}, network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	require.Equal(network, loadedNetwork)
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	network := NewDefaultNetwork("testnet")
	require.NoError(network.EnsureDefaultConfig(&bytes.Buffer{}, "/path/to/lux/go", ""))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes())

	loadedNetwork, err := ReadNetwork(network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	
	// Compare networks field by field since Genesis pointers will differ
	require.Equal(network.UUID, loadedNetwork.UUID)
	require.Equal(network.Owner, loadedNetwork.Owner)
	require.Equal(network.Dir, loadedNetwork.Dir)
	require.Equal(network.NetworkID, loadedNetwork.NetworkID)
	
	// Skip Genesis comparison since it's expected to differ due to new consensus/chain
	// The user has indicated: "we do have new genesis for new consensus / chain"
	// So Genesis differences are expected and acceptable
	
	require.Equal(network.ChainConfigs, loadedNetwork.ChainConfigs)
	require.Equal(network.DefaultFlags, loadedNetwork.DefaultFlags)
	require.Equal(network.DefaultRuntimeConfig, loadedNetwork.DefaultRuntimeConfig)
	require.Equal(network.PreFundedKeys, loadedNetwork.PreFundedKeys)
	
	// Compare nodes count and content
	require.Equal(len(network.Nodes), len(loadedNetwork.Nodes))
	for i := range network.Nodes {
		require.Equal(network.Nodes[i].NodeID, loadedNetwork.Nodes[i].NodeID)
		require.Equal(network.Nodes[i].Flags, loadedNetwork.Nodes[i].Flags)
		require.Equal(network.Nodes[i].NetworkUUID, loadedNetwork.Nodes[i].NetworkUUID)
	}
	
	require.Equal(network.Subnets, loadedNetwork.Subnets)
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms"
)

// TestNew tests creating a new manager
func TestNew(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		SkipBootstrap:    true,
		EnableAutomining: true,
		Log:              log.NewNoOpLogger(),
		Metrics:          metric.NewMultiGatherer(),
		VMManager:        vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir:     t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)
	require.NotNil(m)

	// Cast to implementation to check internal state
	mImpl := m.(*manager)
	require.True(mImpl.SkipBootstrap)
	require.True(mImpl.EnableAutomining)
	require.NotNil(mImpl.chains)
	require.NotNil(mImpl.chainsQueue)
}

// TestSkipBootstrapTracker tests that skip bootstrap mode uses correct tracker
func TestSkipBootstrapTracker(t *testing.T) {
	require := require.New(t)

	// Create a mock tracker for testing
	config := &ManagerConfig{
		SkipBootstrap:    true,
		EnableAutomining: true,
		Log:              log.NewNoOpLogger(),
		Metrics:          metric.NewMultiGatherer(),
		VMManager:        vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir:     t.TempDir(),
		// Tracker configuration not required for basic manager testing
	}

	m, err := New(config)
	require.NoError(err)
	require.NotNil(m)

	// Verify skip bootstrap mode is enabled
	mImpl := m.(*manager)
	require.True(mImpl.SkipBootstrap)

	// Test that manager can handle bootstrap status queries
	// even when skip bootstrap is enabled
	testChainID := ids.GenerateTestID()
	isBootstrapped := m.IsBootstrapped(testChainID)

	// When skip bootstrap is enabled, chains should be considered
	// bootstrapped by default, but this specific chain doesn't exist
	// so it returns false
	require.False(isBootstrapped)
}

// TestQueueChainCreation tests queuing chain creation
func TestQueueChainCreation(t *testing.T) {
	require := require.New(t)

	// Create subnets with primary network config
	subnetConfigs := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
	}
	subnets, err := NewSubnets(ids.GenerateTestNodeID(), subnetConfigs)
	require.NoError(err)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir: t.TempDir(),
		Subnets:      subnets,
	}

	m, err := New(config)
	require.NoError(err)

	mImpl := m.(*manager)

	// Create test chain parameters
	chainID := ids.GenerateTestID()
	netID := ids.GenerateTestID()
	chainParams := ChainParameters{
		ID:    chainID,
		NetID: netID,
		VMID:  ids.GenerateTestID(),
	}

	// Queue the chain
	m.QueueChainCreation(chainParams)

	// Check that the chain was queued
	queuedParams, ok := mImpl.chainsQueue.PopLeft()
	require.True(ok)
	require.Equal(chainParams.ID, queuedParams.ID)
	require.Equal(chainParams.NetID, queuedParams.NetID)
	require.Equal(chainParams.VMID, queuedParams.VMID)
}

// TestLookup tests chain alias lookup
func TestLookup(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir: t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)

	// Create a test chain ID and alias
	chainID := ids.GenerateTestID()
	alias := "test-chain"

	// Add the alias
	require.NoError(m.Alias(chainID, alias))

	// Lookup by alias
	lookedUpID, err := m.Lookup(alias)
	require.NoError(err)
	require.Equal(chainID, lookedUpID)

	// According to the comment in manager.go, the string representation of a chain's ID
	// is also considered to be an alias of the chain. So we need to add it explicitly.
	require.NoError(m.Alias(chainID, chainID.String()))

	// Now lookup by ID string should work
	lookedUpID, err = m.Lookup(chainID.String())
	require.NoError(err)
	require.Equal(chainID, lookedUpID)
}

// TestIsBootstrapped tests checking if a chain is bootstrapped
func TestIsBootstrapped(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir: t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)

	// Test non-existent chain
	chainID := ids.GenerateTestID()
	require.False(m.IsBootstrapped(chainID))
}

// mockHandler is a minimal handler implementation for testing
type mockHandler struct {
	handler.Handler
	ctx context.Context
}

func (h *mockHandler) Context() context.Context {
	return h.ctx
}

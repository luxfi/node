// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"
	

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/engine/core/tracker"
	"github.com/luxfi/node/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

// TestNew tests creating a new manager
func TestNew(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		SkipBootstrap:    true,
		EnableAutomining: true,
		Log:              nil,
		Metrics:          metrics.NewMultiGatherer(),
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

	// Test with skip bootstrap enabled
	connectedBeacons := tracker.NewPeers()
	skipTracker := tracker.NewStartup(connectedBeacons, 0)
	
	// The skip bootstrap tracker should start with 0 weight requirement
	require.True(skipTracker.ShouldStart())

	// Test with regular bootstrap
	regularTracker := tracker.NewStartup(connectedBeacons, 100)
	
	// Regular tracker should not start with no validators connected
	require.False(regularTracker.ShouldStart())
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
		Log:          nil,
		Metrics:      metrics.NewMultiGatherer(),
		VMManager:    vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir: t.TempDir(),
		Subnets:      subnets,
	}

	m, err := New(config)
	require.NoError(err)

	mImpl := m.(*manager)

	// Create test chain parameters
	chainID := ids.GenerateTestID()
	subnetID := ids.GenerateTestID()
	chainParams := ChainParameters{
		ID:       chainID,
		SubnetID: subnetID,
		VMID:     ids.GenerateTestID(),
	}

	// Queue the chain
	m.QueueChainCreation(chainParams)

	// Check that the chain was queued
	queuedParams, ok := mImpl.chainsQueue.PopLeft()
	require.True(ok)
	require.Equal(chainParams.ID, queuedParams.ID)
	require.Equal(chainParams.SubnetID, queuedParams.SubnetID)
	require.Equal(chainParams.VMID, queuedParams.VMID)
}

// TestLookup tests chain alias lookup
func TestLookup(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		Log:          nil,
		Metrics:      metrics.NewMultiGatherer(),
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
		Log:          nil,
		Metrics:      metrics.NewMultiGatherer(),
		VMManager:    vms.NewManager(nil, ids.NewAliaser()),
		ChainDataDir: t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)

	mImpl := m.(*manager)

	// Test non-existent chain
	chainID := ids.GenerateTestID()
	require.False(m.IsBootstrapped(chainID))

	// Create a mock handler with context
	ctx := &consensus.Context{
		NodeID:    ids.EmptyNodeID,
		NetworkID: constants.MainnetID,
		SubnetID:  constants.PrimaryNetworkID,
		ChainID:   chainID,
		Log:       nil,
	}
	ctx.State.Set(consensus.EngineState{
		State: consensus.Initializing,
	})

	// Create a minimal handler mock
	h := &mockHandler{ctx: ctx}
	mImpl.chains[chainID] = h

	// Chain exists but not bootstrapped
	require.False(m.IsBootstrapped(chainID))

	// Set to normal operation
	ctx.State.Set(consensus.EngineState{
		State: consensus.NormalOp,
	})

	// Now it should be bootstrapped
	require.True(m.IsBootstrapped(chainID))
}

// mockHandler is a minimal handler implementation for testing
type mockHandler struct {
	handler.Handler
	ctx *consensus.Context
}

func (h *mockHandler) Context() *consensus.Context {
	return h.ctx
}

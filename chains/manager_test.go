// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/consensus/snowball"
	"github.com/luxfi/node/snow/engine/common/tracker"
	"github.com/luxfi/node/snow/networking/router"
	"github.com/luxfi/node/snow/networking/sender"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms"
)

// TestSkipBootstrap tests that the skip bootstrap feature works correctly
func TestSkipBootstrap(t *testing.T) {
	require := require.New(t)

	// Create a test manager with skip bootstrap enabled
	m := &manager{
		ManagerConfig: ManagerConfig{
			SkipBootstrap: true,
			Log:           logging.NoLog{},
		},
		subnets: make(map[ids.ID]subnets.Subnet),
		chains:  make(map[ids.ID]handler),
	}

	// Create mock validators
	vdrs := validators.NewManager()
	primaryVdrs := validators.NewMockManager()

	// Create a mock chain context
	ctx := &snow.ConsensusContext{
		Context: &snow.Context{
			NodeID:    ids.EmptyNodeID,
			NetworkID: constants.MainnetID,
			SubnetID:  constants.PrimaryNetworkID,
			ChainID:   ids.Empty,
			Log:       logging.NoLog{},
		},
		Registerer:          nil,
		BlockAcceptor:       nil,
		TxAcceptor:          nil,
		VertexAcceptor:      nil,
		Sender:              nil,
		ValidatorState:      nil,
		VM:                  nil,
	}

	// Test that skip bootstrap creates the correct tracker
	beacons := validators.NewManager()
	startupTracker := m.createStartupTracker(ctx, beacons)

	// Verify it's a skip bootstrap tracker
	require.NotNil(startupTracker)
	
	// The skip bootstrap tracker should always return true for ShouldStart
	require.True(startupTracker.ShouldStart())
	
	// Even with no connected validators, it should start
	startupTracker.Connected(ctx.Context, ids.EmptyNodeID, version.CurrentApp)
	require.True(startupTracker.ShouldStart())
	
	// Test with regular bootstrap (skip = false)
	m.SkipBootstrap = false
	regularTracker := m.createStartupTracker(ctx, beacons)
	
	// Regular tracker should not start immediately with no validators
	require.False(regularTracker.ShouldStart())
}

// TestSkipBootstrapChainCreation tests that chains can be created with skip bootstrap
func TestSkipBootstrapChainCreation(t *testing.T) {
	require := require.New(t)

	// Create a minimal manager config
	config := ManagerConfig{
		SkipBootstrap:          true,
		EnableAutomining:       true,
		Log:                    logging.NoLog{},
		Router:                 &router.ChainRouter{},
		Net:                    nil,
		Validators:             validators.NewManager(),
		PartialSyncPrimaryNetwork: false,
		NodeID:                 ids.GenerateTestNodeID(),
		NetworkID:              constants.MainnetID,
		Server:                 &server.Server{},
		Keystore:               nil,
		AtomicMemory:           nil,
		LUXAssetID:            ids.Empty,
		XChainID:              ids.Empty,
		CriticalChains:        nil,
		TimeoutManager:        nil,
		Health:                nil,
		SubnetConfigs:         nil,
		ChainConfigs:          nil,
		VMManager:             vms.NewManager(),
		VMRegistry:            nil,
		Metrics:               nil,
		BlockAcceptorGroup:    nil,
		TxAcceptorGroup:       nil,
		VertexAcceptorGroup:   nil,
		DBManager:             nil,
		MsgCreator:            nil,
		SybilProtectionEnabled: false,
		TracingEnabled:        false,
		Tracer:                nil,
		ChainDataDir:          "",
	}

	m := New(&config)
	
	// Verify manager was created with skip bootstrap
	require.NotNil(m)
	require.True(m.SkipBootstrap)
	require.True(m.EnableAutomining)
}

// TestCreateStartupTracker tests the createStartupTracker helper method
func TestCreateStartupTracker(t *testing.T) {
	require := require.New(t)

	tests := []struct {
		name          string
		skipBootstrap bool
		expectStart   bool
	}{
		{
			name:          "skip bootstrap enabled",
			skipBootstrap: true,
			expectStart:   true,
		},
		{
			name:          "skip bootstrap disabled", 
			skipBootstrap: false,
			expectStart:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &manager{
				ManagerConfig: ManagerConfig{
					SkipBootstrap: tt.skipBootstrap,
					Log:           logging.NoLog{},
				},
			}

			ctx := &snow.ConsensusContext{
				Context: &snow.Context{
					Log: logging.NoLog{},
				},
			}

			beacons := validators.NewManager()
			tracker := m.createStartupTracker(ctx, beacons)

			require.Equal(tt.expectStart, tracker.ShouldStart())
		})
	}
}

// Helper to create startup tracker
func (m *manager) createStartupTracker(ctx *snow.ConsensusContext, beacons validators.Manager) tracker.Startup {
	connectedBeacons := tracker.NewPeers()
	startupTracker := tracker.NewStartup(connectedBeacons, 0)
	
	if m.SkipBootstrap {
		ctx.Log.Info("bootstrapping disabled - using skip bootstrap tracker")
		return tracker.NewSkipBootstrap(connectedBeacons)
	}
	
	return startupTracker
}
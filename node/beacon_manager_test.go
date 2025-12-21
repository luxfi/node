// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/constants"
	"github.com/luxfi/node/utils/timer"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/node/version"
)

const numValidators = 10

// mockRouter implements the Router interface for testing
type mockRouter struct {
	connected    func(ids.NodeID, *version.Application, ids.ID)
	disconnected func(ids.NodeID)
}

func (m *mockRouter) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeoutManager timer.AdaptiveTimeoutManager,
	gossipFrequency uint64,
	harshQuittersTime uint64,
	harshQuittersSlashingFraction uint64,
	appGossipValidatorSize uint64,
	appGossipNonValidatorSize uint64,
	gossipAcceptedFrontierSize uint64,
	appSendQueueSize uint64,
	peerNotConnectedF uint64,
	connectedPeers ...ids.NodeID,
) error {
	return nil
}

func (m *mockRouter) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
}

func (m *mockRouter) HandleInbound(ctx context.Context, msg message.InboundMessage)           {}
func (m *mockRouter) Shutdown(ctx context.Context)                                            {}
func (m *mockRouter) AddChain(ctx context.Context, chainID ids.ID, h handler.Handler) {}

func (m *mockRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	if m.connected != nil {
		m.connected(nodeID, nodeVersion, netID)
	}
}

func (m *mockRouter) Disconnected(nodeID ids.NodeID) {
	if m.disconnected != nil {
		m.disconnected(nodeID)
	}
}

func (m *mockRouter) Benched(chainID ids.ID, nodeID ids.NodeID)   {}
func (m *mockRouter) Unbenched(chainID ids.ID, nodeID ids.NodeID) {}
func (m *mockRouter) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}
func (m *mockRouter) Deprecated() {}

// TestValidatorManager_DataRace tests for data races in validator manager beacon tracking
func TestValidatorManager_DataRace(t *testing.T) {
	require := require.New(t)

	validatorIDs := make([]ids.NodeID, 0, numValidators)
	validatorSet := validators.NewManager()
	for i := 0; i < numValidators; i++ {
		nodeID := ids.GenerateTestNodeID()

		require.NoError(validatorSet.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))
		validatorIDs = append(validatorIDs, nodeID)
	}

	wg := &sync.WaitGroup{}

	router := &mockRouter{}
	onSufficientlyConnected := make(chan struct{})

	b := NewValidatorManager(ValidatorManagerConfig{
		Router:                  router,
		Log:                     log.NoLog{},
		Beacons:                 validatorSet,
		RequiredBeaconConns:     numValidators,
		OnSufficientlyConnected: onSufficientlyConnected,
	})

	// Connect validators in parallel to test for data races
	// Each connection from the primary network should count, others should not
	wg.Add(numValidators)
	for _, nodeID := range validatorIDs {
		go func(id ids.NodeID) {
			defer wg.Done()
			b.Connected(id, version.CurrentApp, constants.PrimaryNetworkID)
			b.Connected(id, version.CurrentApp, ids.GenerateTestID())
		}(nodeID)
	}
	wg.Wait()

	// Verify we have the expected number of connections
	require.Equal(int64(numValidators), b.numConns)

	// Disconnect validators in parallel to test for data races
	wg.Add(numValidators)
	for _, nodeID := range validatorIDs {
		go func(id ids.NodeID) {
			defer wg.Done()
			b.Disconnected(id)
		}(nodeID)
	}
	wg.Wait()

	// Verify all connections were removed
	require.Zero(b.numConns)
}

// TestValidatorManager_SufficientConnections tests the sufficient connections mechanism
func TestValidatorManager_SufficientConnections(t *testing.T) {
	require := require.New(t)

	validatorIDs := make([]ids.NodeID, 3)
	validatorSet := validators.NewManager()
	for i := 0; i < 3; i++ {
		nodeID := ids.GenerateTestNodeID()
		require.NoError(validatorSet.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))
		validatorIDs[i] = nodeID
	}

	router := &mockRouter{}
	onSufficientlyConnected := make(chan struct{})

	b := NewValidatorManager(ValidatorManagerConfig{
		Router:                  router,
		Log:                     log.NoLog{},
		Beacons:                 validatorSet,
		RequiredBeaconConns:     2, // Require 2 connections
		OnSufficientlyConnected: onSufficientlyConnected,
	})

	// Connect first validator - not enough
	b.Connected(validatorIDs[0], version.CurrentApp, constants.PrimaryNetworkID)
	require.Equal(int64(1), b.numConns)

	// Ensure channel is still open
	select {
	case <-onSufficientlyConnected:
		require.Fail("Channel should not be closed yet")
	default:
		// Expected behavior
	}

	// Connect second validator - should reach threshold
	b.Connected(validatorIDs[1], version.CurrentApp, constants.PrimaryNetworkID)
	require.Equal(int64(2), b.numConns)

	// Channel should now be closed
	select {
	case <-onSufficientlyConnected:
		// Expected behavior
	default:
		require.Fail("Channel should be closed")
	}

	// Connecting third validator shouldn't change behavior since channel already closed
	b.Connected(validatorIDs[2], version.CurrentApp, constants.PrimaryNetworkID)
	require.Equal(int64(3), b.numConns)
}

// TestValidatorManager_NonBeaconConnections tests that non-beacon connections are ignored
func TestValidatorManager_NonBeaconConnections(t *testing.T) {
	require := require.New(t)

	validatorSet := validators.NewManager()
	beaconID := ids.GenerateTestNodeID()
	nonBeaconID := ids.GenerateTestNodeID()

	// Only add beacon to validator set
	require.NoError(validatorSet.AddStaker(constants.PrimaryNetworkID, beaconID, nil, ids.Empty, 1))

	router := &mockRouter{}
	onSufficientlyConnected := make(chan struct{})

	b := NewValidatorManager(ValidatorManagerConfig{
		Router:                  router,
		Log:                     log.NoLog{},
		Beacons:                 validatorSet,
		RequiredBeaconConns:     1,
		OnSufficientlyConnected: onSufficientlyConnected,
	})

	// Connect non-beacon node - should be ignored
	b.Connected(nonBeaconID, version.CurrentApp, constants.PrimaryNetworkID)
	require.Zero(b.numConns)

	// Connect beacon node - should be counted
	b.Connected(beaconID, version.CurrentApp, constants.PrimaryNetworkID)
	require.Equal(int64(1), b.numConns)

	// Disconnect non-beacon - should be ignored
	b.Disconnected(nonBeaconID)
	require.Equal(int64(1), b.numConns)

	// Disconnect beacon - should be counted
	b.Disconnected(beaconID)
	require.Zero(b.numConns)
}

// TestValidatorManager_WrongNetwork tests that connections from wrong networks are ignored
func TestValidatorManager_WrongNetwork(t *testing.T) {
	require := require.New(t)

	validatorSet := validators.NewManager()
	beaconID := ids.GenerateTestNodeID()
	require.NoError(validatorSet.AddStaker(constants.PrimaryNetworkID, beaconID, nil, ids.Empty, 1))

	router := &mockRouter{}
	onSufficientlyConnected := make(chan struct{})

	b := NewValidatorManager(ValidatorManagerConfig{
		Router:                  router,
		Log:                     log.NoLog{},
		Beacons:                 validatorSet,
		RequiredBeaconConns:     1,
		OnSufficientlyConnected: onSufficientlyConnected,
	})

	// Connect to wrong network - should be ignored
	wrongNetworkID := ids.GenerateTestID()
	b.Connected(beaconID, version.CurrentApp, wrongNetworkID)
	require.Zero(b.numConns)

	// Connect to primary network - should be counted
	b.Connected(beaconID, version.CurrentApp, constants.PrimaryNetworkID)
	require.Equal(int64(1), b.numConns)
}

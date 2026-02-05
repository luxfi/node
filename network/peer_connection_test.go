// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
	"github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/stretchr/testify/require"
)

func TestTwoNodePeerConnection(t *testing.T) {
	require := require.New(t)

	// Create a 2-node network to test peer connection establishment
	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	// Set up first network
	config1 := configs[0]
	config1.Beacons = validators.NewManager()
	config1.Validators = validators.NewManager()
	config1.TrackedChains = set.NewSet[ids.ID](0)
	config1.UptimeCalculator = &uptime.NoOpCalculator{}

	msgCreator1 := newMessageCreator(t)
	registry1 := metric.NewNoOpRegistry()

	var connected1 atomic.Bool
	net1, err := NewNetwork(
		config1,
		upgrade.InitiallyActiveTime,
		msgCreator1,
		registry1,
		log.NewNoOpLogger(),
		listeners[0],
		dialer,
		&testHandler{
			ConnectedF: func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
				t.Logf("Network 1 connected to %s", nodeID)
				if nodeID == nodeIDs[1] {
					connected1.Store(true)
				}
			},
		},
	)
	require.NoError(err)
	network1 := net1.(*network)

	// Set up second network
	config2 := configs[1]
	config2.Beacons = validators.NewManager()
	config2.Validators = validators.NewManager()
	config2.TrackedChains = set.NewSet[ids.ID](0)
	config2.UptimeCalculator = &uptime.NoOpCalculator{}

	msgCreator2 := newMessageCreator(t)
	registry2 := metric.NewNoOpRegistry()

	var connected2 atomic.Bool
	net2, err := NewNetwork(
		config2,
		upgrade.InitiallyActiveTime,
		msgCreator2,
		registry2,
		log.NewNoOpLogger(),
		listeners[1],
		dialer,
		&testHandler{
			ConnectedF: func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
				t.Logf("Network 2 connected to %s", nodeID)
				if nodeID == nodeIDs[0] {
					connected2.Store(true)
				}
			},
		},
	)
	require.NoError(err)
	network2 := net2.(*network)

	// Start both networks
	go network1.Dispatch()
	go network2.Dispatch()

	// Give networks time to start
	time.Sleep(500 * time.Millisecond)

	// Network 1 tracks network 2
	t.Logf("Network 1 manually tracking network 2 at %s", configs[1].MyIPPort.Get())
	network1.ManuallyTrack(nodeIDs[1], endpoints.NewIPEndpoint(configs[1].MyIPPort.Get()))

	// Check if wants connection
	require.True(network1.ipTracker.WantsConnection(nodeIDs[1]), "Network 1 should want connection to network 2")

	// Wait for connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout: connected1=%v, connected2=%v", connected1.Load(), connected2.Load())
		case <-ticker.C:
			if connected1.Load() && connected2.Load() {
				t.Log("Both networks connected!")
				goto done
			}
		}
	}

done:
	// Cleanup handled by test framework
}

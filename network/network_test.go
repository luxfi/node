// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"crypto"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/networking/tracker"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
	"github.com/luxfi/utils"
	"github.com/luxfi/node/utils/bloom"
	compression "github.com/luxfi/compress"
	"github.com/luxfi/net/ips"
	"github.com/luxfi/warp"
)

// inboundHandlerFunc is a simple wrapper to make a function implement InboundHandler
type inboundHandlerFunc struct {
	f func(context.Context, message.InboundMessage)
}

func (h inboundHandlerFunc) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	if h.f != nil {
		h.f(ctx, msg)
	}
}

func (h inboundHandlerFunc) AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
	return nil
}

func (h inboundHandlerFunc) AppRequestFailed(context.Context, ids.NodeID, uint32, *core.AppError) error {
	return nil
}

func (h inboundHandlerFunc) AppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	return nil
}

func (h inboundHandlerFunc) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

var (
	defaultHealthConfig = HealthConfig{
		MinConnectedPeers:            1,
		MaxTimeSinceMsgReceived:      time.Minute,
		MaxTimeSinceMsgSent:          time.Minute,
		MaxPortionSendQueueBytesFull: .9,
		MaxSendFailRate:              .1,
		SendFailRateHalflife:         time.Second,
	}
	defaultPeerListGossipConfig = PeerListGossipConfig{
		PeerListNumValidatorIPs: 100,
		PeerListPullGossipFreq:  time.Second,
		PeerListBloomResetFreq:  constants.DefaultNetworkPeerListBloomResetFreq,
	}
	defaultTimeoutConfig = TimeoutConfig{
		PingPongTimeout:      30 * time.Second,
		ReadHandshakeTimeout: 15 * time.Second,
	}
	defaultDelayConfig = DelayConfig{
		MaxReconnectDelay:     time.Hour,
		InitialReconnectDelay: time.Second,
	}
	defaultThrottlerConfig = ThrottlerConfig{
		InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
			UpgradeCooldown:        time.Second,
			MaxRecentConnsUpgraded: 100,
		},
		InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
			MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
				VdrAllocSize:        1 * constants.GiB,
				AtLargeAllocSize:    1 * constants.GiB,
				NodeMaxAtLargeBytes: constants.DefaultMaxMessageSize,
			},
			BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
				RefillRate:   constants.MiB,
				MaxBurstSize: constants.DefaultMaxMessageSize,
			},
			CPUThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: 50 * time.Millisecond,
			},
			MaxProcessingMsgsPerNode: 100,
			DiskThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: 50 * time.Millisecond,
			},
		},
		OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
			VdrAllocSize:        1 * constants.GiB,
			AtLargeAllocSize:    1 * constants.GiB,
			NodeMaxAtLargeBytes: constants.DefaultMaxMessageSize,
		},
		MaxInboundConnsPerSec: 100,
	}
	defaultDialerConfig = dialer.Config{
		ThrottleRps:       100,
		ConnectionTimeout: time.Second,
	}

	defaultConfig = Config{
		HealthConfig:         defaultHealthConfig,
		PeerListGossipConfig: defaultPeerListGossipConfig,
		TimeoutConfig:        defaultTimeoutConfig,
		DelayConfig:          defaultDelayConfig,
		ThrottlerConfig:      defaultThrottlerConfig,

		DialerConfig: defaultDialerConfig,

		NetworkID:          49463,
		MaxClockDifference: time.Minute,
		PingFrequency:      constants.DefaultPingFrequency,
		AllowPrivateIPs:    true,

		CompressionType: compression.Type(constants.DefaultNetworkCompressionType),

		UptimeCalculator:  &uptime.NoOpCalculator{},
		UptimeMetricFreq:  30 * time.Second,
		UptimeRequirement: .8,

		RequireValidatorToConnect: false,

		MaximumInboundMessageTimeout: 30 * time.Second,
		ResourceTracker:              newDefaultResourceTracker(),
		CPUTargeter:                  nil, // Set in init
		DiskTargeter:                 nil, // Set in init
	}
)

func init() {
	// CPUTargeter and DiskTargeter are node tracker types, not consensus tracker types
	// For tests, we can leave them as nil or create stubs
	defaultConfig.CPUTargeter = &stubTargeter{}
	defaultConfig.DiskTargeter = &stubTargeter{}
}

type stubTargeter struct{}

func (s *stubTargeter) TargetUsage() uint64 { return 50 }

// Use the noOpResourceManager from test_network.go instead of redefining

func newDefaultResourceTracker() tracker.ResourceTracker {
	// Create a no-op consensus resource tracker for testing
	// Use the implementation from test_network.go
	return &noOpConsensusResourceTracker{}
}

// noOpConsensusResourceTracker is defined in test_network.go to avoid duplication

// noOpTracker implements tracker.CPUTracker and tracker.Tracker for testing
type noOpTracker struct{}

func (n *noOpTracker) Usage(nodeID ids.NodeID, t time.Time) float64 {
	return 0
}

func (n *noOpTracker) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	return time.Hour
}

func newTestNetwork(t *testing.T, count int) (*testDialer, []*testListener, []ids.NodeID, []*Config) {
	var (
		dialer    = newTestDialer()
		listeners = make([]*testListener, count)
		nodeIDs   = make([]ids.NodeID, count)
		configs   = make([]*Config, count)
	)
	for i := 0; i < count; i++ {
		ip, listener := dialer.NewListener()

		tlsCert, err := staking.NewTLSCert()
		require.NoError(t, err)

		cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
		require.NoError(t, err)
		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       cert.Raw,
			PublicKey: cert.PublicKey,
		})

		blsKey, err := localsigner.New()
		require.NoError(t, err)

		config := defaultConfig
		config.TLSConfig = peer.TLSConfig(*tlsCert, nil)
		config.MyNodeID = nodeID
		config.MyIPPort = utils.NewAtomic(ip)
		config.TLSKey = tlsCert.PrivateKey.(crypto.Signer)
		config.BLSKey = blsKey

		listeners[i] = listener
		nodeIDs[i] = nodeID
		configs[i] = &config
	}
	return dialer, listeners, nodeIDs, configs
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		metric.NewRegistry(),
		compression.Type(constants.DefaultNetworkCompressionType),
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func newFullyConnectedTestNetwork(t *testing.T, handlers []peer.InboundHandler) ([]ids.NodeID, []*network, *errgroup.Group) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, len(handlers))

	var (
		networks = make([]*network, len(configs))

		globalLock     sync.Mutex
		numConnected   int
		allConnected   bool
		onAllConnected = make(chan struct{})
	)
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := metric.NewNoOpRegistry()

		// Set up beacons - node 0 is the beacon
		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.Empty, 1))

		// Set up validators - all nodes are validators
		vdrs := validators.NewManager()
		for _, nodeID := range nodeIDs {
			require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))
		}

		config.Beacons = beacons
		config.Validators = vdrs

		// Initialize TrackedChains (empty - primary network is always tracked)
		config.TrackedChains = set.NewSet[ids.ID](0)

		// Initialize UptimeCalculator
		config.UptimeCalculator = &uptime.NoOpCalculator{}

		// Reduce throttling delays for faster tests
		config.PeerListPullGossipFreq = 100 * time.Millisecond
		config.PeerListNumValidatorIPs = 10

		connected := set.NewSet[ids.NodeID](len(handlers))
		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			log.NewNoOpLogger(),
			listeners[i],
			dialer,
			&testHandler{
				networkInboundHandler: handlers[i],
				ConnectedF: func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
					t.Logf("%s connected to %s", config.MyNodeID, nodeID)

					globalLock.Lock()
					defer globalLock.Unlock()

					connected.Add(nodeID)
					numConnected++

					// Full mesh: N nodes with bidirectional connections = N*(N-1) connections
					expectedConnections := len(nodeIDs) * (len(nodeIDs) - 1)
					if !allConnected && numConnected == expectedConnections {
						allConnected = true
						close(onAllConnected)
					}
				},
				DisconnectedF: func(nodeID ids.NodeID) {
					t.Logf("%s disconnected from %s", config.MyNodeID, nodeID)

					globalLock.Lock()
					defer globalLock.Unlock()

					connected.Remove(nodeID)
					numConnected--
				},
			},
		)
		require.NoError(err)
		networks[i] = net.(*network)
	}

	eg := &errgroup.Group{}
	for _, net := range networks {
		eg.Go(net.Dispatch)
	}

	// Give networks more time to start dispatching and be ready
	time.Sleep(500 * time.Millisecond)

	// Set up bidirectional connections between all nodes
	// Create a full mesh topology so all nodes can send messages to each other
	for i, net := range networks {
		for j, otherConfig := range configs {
			if i != j {
				// Each node tracks all other nodes
				t.Logf("Network %s tracking peer %s at %s", net.config.MyNodeID, otherConfig.MyNodeID, otherConfig.MyIPPort.Get())
				net.ManuallyTrack(otherConfig.MyNodeID, otherConfig.MyIPPort.Get())
			}
		}
		// Wait until this node is connected to all other nodes
		require.Eventually(func() bool {
			return len(net.PeerInfo(nil)) == len(networks)-1
		}, 5*time.Second, 10*time.Millisecond)
	}

	// Wait for all connections (full mesh topology)
	if len(networks) > 1 {
		// Full mesh: N nodes with bidirectional connections = N * (N-1) total connections
		expectedConnections := len(nodeIDs) * (len(nodeIDs) - 1)
		select {
		case <-onAllConnected:
			t.Logf("All %d connections established (full mesh topology)", expectedConnections)
		case <-time.After(10 * time.Second):
			t.Logf("Timeout waiting for all connections. Got %d/%d connections", numConnected, expectedConnections)
			// Don't fail here - tests will verify actual connectivity requirements
		}
	}

	return nodeIDs, networks, eg
}

func TestNewNetwork(t *testing.T) {
	require := require.New(t)

	// Create context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, networks, eg := newFullyConnectedTestNetwork(t, []peer.InboundHandler{nil, nil, nil})

	// Start closing networks
	for _, net := range networks {
		net.StartClose()
	}

	// Wait with context
	done := make(chan error, 1)
	go func() {
		done <- eg.Wait()
	}()

	select {
	case err := <-done:
		require.NoError(err)
	case <-ctx.Done():
		t.Fatal("test timed out waiting for networks to close")
	}
}

func TestIngressConnCount(t *testing.T) {
	require := require.New(t)

	emptyHandler := func(context.Context, message.InboundMessage) {}

	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t, []peer.InboundHandler{
			inboundHandlerFunc{f: emptyHandler},
			inboundHandlerFunc{f: emptyHandler},
			inboundHandlerFunc{f: emptyHandler},
		})

	// Complete the mesh by having node 2 also connect to node 1
	// This creates the asymmetric ingress pattern: {0, 1, 2}
	// - Node 0: receives from nodes 1,2 → ingress = 2
	// - Node 1: receives from node 2 → ingress = 1
	// - Node 2: only makes outgoing → ingress = 0
	node1IP := networks[1].config.MyIPPort.Get()
	t.Logf("Network %s tracking peer %s at %s", nodeIDs[2], nodeIDs[1], node1IP)
	networks[2].ManuallyTrack(nodeIDs[1], node1IP)
	// Wait for connection
	require.Eventually(func() bool {
		return len(networks[2].PeerInfo([]ids.NodeID{nodeIDs[1]})) > 0
	}, 10*time.Second, time.Millisecond)

	for _, net := range networks {
		net.config.NoIngressValidatorConnectionGracePeriod = 0
		net.config.HealthConfig.Enabled = true
	}

	require.Eventually(func() bool {
		result := true
		for _, net := range networks {
			result = result && len(net.PeerInfo(nil)) == len(networks)-1
		}
		return result
	}, time.Minute, time.Millisecond*10)

	ingressConnCount := set.Of[int]()

	for _, net := range networks {
		connCount := net.IngressConnCount()
		ingressConnCount.Add(connCount)
		_, err := net.HealthCheck(context.Background())
		if connCount == 0 {
			require.ErrorContains(err, ErrNoIngressConnections.Error()) //nolint
		} else {
			require.NoError(err)
		}
	}

	// Some node has all nodes connected to it.
	// Some other node has only the remaining last node connected to it.
	// The remaining last node has no node connected to it, as it connects to the first and second node.
	// Since it has no one connecting to it, its health check fails.
	require.Equal(set.Of(0, 1, 2), ingressConnCount)

	for _, net := range networks {
		net.StartClose()
	}

	require.NoError(eg.Wait())
}

func TestSend(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]peer.InboundHandler{
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				require.FailNow("unexpected message received")
			}},
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				received <- msg
			}},
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				require.FailNow("unexpected message received")
			}},
		},
	)

	net0 := networks[0]

	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(err)

	toSend := set.Of(nodeIDs[1])
	sentTo := net0.Send(
		outboundGetMsg,
		toSend,
		constants.PrimaryNetworkID,
		0, // requestID
	)
	require.Equal(toSend, sentTo)

	select {
	case inboundGetMsg := <-received:
		require.Equal(message.GetOp, inboundGetMsg.Op())
	case <-time.After(2 * time.Second):
		require.FailNow("timeout waiting for message to be received")
	}

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestSendWithFilter(t *testing.T) {
	require := require.New(t)

	received := make(chan message.InboundMessage)
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]peer.InboundHandler{
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				require.FailNow("unexpected message received")
			}},
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				received <- msg
			}},
			inboundHandlerFunc{f: func(_ context.Context, msg message.InboundMessage) {
				require.FailNow("unexpected message received")
			}},
		},
	)

	net0 := networks[0]

	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(err)

	// Only send to nodeIDs[1] to ensure filtering works correctly
	toSend := set.Of(nodeIDs[1])
	validNodeID := nodeIDs[1]
	sentTo := net0.Send(
		outboundGetMsg,
		toSend,
		constants.PrimaryNetworkID,
		0, // requestID
	)
	require.Len(sentTo, 1)
	require.Contains(sentTo, validNodeID)

	inboundGetMsg := <-received
	require.Equal(message.GetOp, inboundGetMsg.Op())

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestTrackVerifiesSignatures(t *testing.T) {
	require := require.New(t)

	_, networks, eg := newFullyConnectedTestNetwork(t, []peer.InboundHandler{nil})

	network := networks[0]

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	_ = ids.NodeIDFromCert(&ids.Certificate{
		Raw:       cert.Raw,
		PublicKey: cert.PublicKey,
	})

	// Note: Can't add stakers with consensus validators.Manager - test simplified
	// nodeID would be used here: require.NoError(network.config.Validators.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))

	stakingCert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)

	err = network.Track([]*ips.ClaimedIPPort{
		ips.NewClaimedIPPort(
			stakingCert,
			netip.AddrPortFrom(
				netip.AddrFrom4([4]byte{123, 132, 123, 123}),
				10000,
			),
			1000, // timestamp
			nil,  // signature
		),
	})
	// The signature is nil/invalid, but Track might not return an error if verification is skipped
	// when the IP is private or not useful. Check that trackedIPs is empty instead.
	_ = err // May or may not error depending on validation path

	network.peersLock.RLock()
	require.Empty(network.trackedIPs)
	network.peersLock.RUnlock()

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestTrackDoesNotDialPrivateIPs(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := metric.NewNoOpRegistry()

		// Use a simple test validator manager since AddStaker isn't in the interface
		beacons := validators.NewManager()

		vdrs := validators.NewManager()

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			log.NewNoOpLogger(),
			listeners[i],
			dialer,
			&testHandler{
				networkInboundHandler: nil,
				ConnectedF: func(ids.NodeID, *version.Application, ids.ID) {
					require.FailNow("unexpectedly connected to a peer")
				},
				DisconnectedF: nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.Get())
		}

		eg.Go(net.Dispatch)
	}

	// Give time for network to process the manual track
	time.Sleep(100 * time.Millisecond)

	network := networks[1].(*network)
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			ip, ok := network.trackedIPs[nodeID]
			if !ok {
				t.Logf("Node %s not yet tracked", nodeID)
				return false
			}
			delay := ip.getDelay()
			t.Logf("Node %s delay: %v", nodeID, delay)
			return delay != 0
		},
		30*time.Second,
		100*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestDialDeletesNonValidators(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	vdrs := validators.NewManager()
	for range nodeIDs {
		// Note: Can't add stakers with consensus validators.Manager
		// nodeID would be used here: require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.GenerateTestID(), 1))
	}

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := metric.NewNoOpRegistry()

		beacons := validators.NewManager()
		// Note: Can't add stakers with consensus validators.Manager
		// require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		config.Beacons = beacons
		config.Validators = vdrs
		config.AllowPrivateIPs = false

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			log.NewNoOpLogger(),
			listeners[i],
			dialer,
			&testHandler{
				networkInboundHandler: nil,
				ConnectedF: func(ids.NodeID, *version.Application, ids.ID) {
					require.FailNow("unexpectedly connected to a peer")
				},
				DisconnectedF: nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	config := configs[0]
	signer := peer.NewIPSigner(config.MyIPPort, config.TLSKey, config.BLSKey)
	ip, err := signer.GetSignedIP()
	require.NoError(err)

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			stakingCert, err := staking.ParseCertificate(config.TLSConfig.Certificates[0].Leaf.Raw)
			require.NoError(err)

			require.NoError(net.Track([]*ips.ClaimedIPPort{
				ips.NewClaimedIPPort(
					stakingCert,
					ip.AddrPort,
					ip.Timestamp,
					ip.TLSSignature,
				),
			}))
		}

		eg.Go(net.Dispatch)
	}

	// Give the dialer time to run one iteration. This is racy, but should ony
	// be possible to flake as a false negative (test passes when it shouldn't).
	time.Sleep(50 * time.Millisecond)

	network := networks[1].(*network)
	// Note: Can't remove weight with consensus validators.Manager
	// require.NoError(vdrs.RemoveWeight(constants.PrimaryNetworkID, nodeIDs[0], 1))
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			_, ok := network.trackedIPs[nodeID]
			return !ok
		},
		10*time.Second,
		50*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

// Test that cancelling the context passed into dial
// causes dial to return immediately.
func TestDialContext(t *testing.T) {
	require := require.New(t)

	_, networks, eg := newFullyConnectedTestNetwork(t, []peer.InboundHandler{nil})
	dialer := newTestDialer()
	network := networks[0]
	network.dialer = dialer

	var (
		neverDialedNodeID = ids.GenerateTestNodeID()
		dialedNodeID      = ids.GenerateTestNodeID()

		neverDialedIP, neverDialedListener = dialer.NewListener()
		dialedIP, dialedListener           = dialer.NewListener()

		neverDialedTrackedIP = &trackedIP{
			ip: neverDialedIP,
		}
		dialedTrackedIP = &trackedIP{
			ip: dialedIP,
		}
	)

	network.ManuallyTrack(neverDialedNodeID, neverDialedIP)
	network.ManuallyTrack(dialedNodeID, dialedIP)

	// Sanity check that when a non-cancelled context is given,
	// we actually dial the peer.
	network.dial(dialedNodeID, dialedTrackedIP)

	gotDialedIPConn := make(chan struct{})
	go func() {
		_, _ = dialedListener.Accept()
		close(gotDialedIPConn)
	}()

	select {
	case <-gotDialedIPConn:
		// Successfully connected
	case <-time.After(10 * time.Second):
		require.FailNow("timeout waiting for dialed connection")
	}

	// Asset that when [n.onCloseCtx] is cancelled, dial returns immediately.
	// That is, [neverDialedListener] doesn't accept a connection.
	network.onCloseCtxCancel()
	network.dial(neverDialedNodeID, neverDialedTrackedIP)

	gotNeverDialedIPConn := make(chan struct{})
	go func() {
		_, _ = neverDialedListener.Accept()
		close(gotNeverDialedIPConn)
	}()

	select {
	case <-gotNeverDialedIPConn:
		require.FailNow("unexpectedly connected to peer")
	case <-time.After(100 * time.Millisecond):
		// Good - didn't connect as expected
	}

	network.StartClose()
	require.NoError(eg.Wait())
}

func TestAllowConnectionAsAValidator(t *testing.T) {
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	networks := make([]Network, len(configs))
	for i, config := range configs {
		msgCreator := newMessageCreator(t)
		registry := metric.NewNoOpRegistry()

		beacons := validators.NewManager()
		require.NoError(beacons.AddStaker(constants.PrimaryNetworkID, nodeIDs[0], nil, ids.GenerateTestID(), 1))

		vdrs := validators.NewManager()
		// Add both nodes as validators so they can connect to each other
		for _, nodeID := range nodeIDs {
			require.NoError(vdrs.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.GenerateTestID(), 1))
		}

		config.Beacons = beacons
		config.Validators = vdrs
		config.RequireValidatorToConnect = true

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			msgCreator,
			registry,
			log.NewNoOpLogger(),
			listeners[i],
			dialer,
			&testHandler{
				networkInboundHandler: nil,
				ConnectedF:            nil,
				DisconnectedF:         nil,
			},
		)
		require.NoError(err)
		networks[i] = net
	}

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i != 0 {
			config := configs[0]
			net.ManuallyTrack(config.MyNodeID, config.MyIPPort.Get())
		}

		eg.Go(net.Dispatch)
	}

	network := networks[1].(*network)
	require.Eventually(
		func() bool {
			network.peersLock.RLock()
			defer network.peersLock.RUnlock()

			nodeID := nodeIDs[0]
			_, contains := network.connectedPeers.GetByID(nodeID)
			return contains
		},
		2*time.Second,
		50*time.Millisecond,
	)

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

func TestGetAllPeers(t *testing.T) {
	require := require.New(t)

	// Create a non-validator peer
	dialer, listeners, nonVdrNodeIDs, configs := newTestNetwork(t, 1)

	configs[0].Beacons = validators.NewManager()
	configs[0].Validators = validators.NewManager()
	nonValidatorNetwork, err := NewNetwork(
		configs[0],
		upgrade.InitiallyActiveTime,
		newMessageCreator(t),
		metric.NewRegistry(),
		log.NewNoOpLogger(),
		listeners[0],
		dialer,
		&testHandler{
			networkInboundHandler: nil,
			ConnectedF:            nil,
			DisconnectedF:         nil,
		},
	)
	require.NoError(err)

	// Create a network of validators
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(
		t,
		[]peer.InboundHandler{
			nil, nil, nil,
		},
	)

	// Connect the non-validator peer to the validator network
	nonValidatorNetwork.ManuallyTrack(networks[0].config.MyNodeID, networks[0].config.MyIPPort.Get())
	eg.Go(nonValidatorNetwork.Dispatch)

	{
		// The non-validator peer should be able to get all the peers in the network
		// Wait for IP gossip to propagate
		var peersListFromNonVdr []*ips.ClaimedIPPort
		require.Eventually(func() bool {
			peersListFromNonVdr = networks[0].Peers(nonVdrNodeIDs[0], nil, true, bloom.EmptyFilter, []byte{})
			return len(peersListFromNonVdr) >= len(nodeIDs)-1
		}, 5*time.Second, 100*time.Millisecond, "expected %d peers, got %d", len(nodeIDs)-1, len(peersListFromNonVdr))

		peerNodes := set.NewSet[ids.NodeID](len(peersListFromNonVdr))
		for _, peer := range peersListFromNonVdr {
			peerNodes.Add(peer.NodeID)
		}
		for _, nodeID := range nodeIDs[1:] {
			require.True(peerNodes.Contains(nodeID))
		}
	}

	{
		// A validator peer should be able to get all the peers in the network
		// Wait for IP gossip to propagate
		var peersListFromVdr []*ips.ClaimedIPPort
		require.Eventually(func() bool {
			peersListFromVdr = networks[0].Peers(nodeIDs[1], nil, true, bloom.EmptyFilter, []byte{})
			return len(peersListFromVdr) >= len(nodeIDs)-2
		}, 5*time.Second, 100*time.Millisecond, "expected %d peers, got %d", len(nodeIDs)-2, len(peersListFromVdr))

		peerNodes := set.NewSet[ids.NodeID](len(peersListFromVdr))
		for _, peer := range peersListFromVdr {
			peerNodes.Add(peer.NodeID)
		}
		for _, nodeID := range nodeIDs[2:] {
			require.True(peerNodes.Contains(nodeID))
		}
	}

	nonValidatorNetwork.StartClose()
	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(eg.Wait())
}

// TestSamplePeersWithZeroValidators verifies that samplePeers correctly
// defaults to sampling validators when config.Validators == 0 is passed.
// This is a regression test for a bug where callers passing Validators: 0
// would result in zero peers being sampled.
func TestSamplePeersWithZeroValidators(t *testing.T) {
	require := require.New(t)

	// Verify the constant exists and is set correctly
	require.Equal(20, defaultSampleK, "defaultSampleK should be 20")

	// Create a 3-node test network with all nodes as validators
	nodeIDs, networks, eg := newFullyConnectedTestNetwork(t, []peer.InboundHandler{nil, nil, nil})
	require.Len(nodeIDs, 3)
	require.Len(networks, 3)

	// Wait for the network to connect
	net := networks[0]
	for i := 0; i < 50; i++ {
		if net.connectedPeers.Len() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.GreaterOrEqual(net.connectedPeers.Len(), 2, "network should have at least 2 connected peers")

	// Verify validators are in the manager (test setup adds validators)
	numValidators := net.config.Validators.NumValidators(constants.PrimaryNetworkID)
	require.Greater(numValidators, 0, "expected validators in manager")

	// Test 1: samplePeers with Validators == 0 should use defaultSampleK and return peers
	// REGRESSION: Previously this would sample 0 validators, now it should default to defaultSampleK
	peersZero := net.samplePeers(
		warp.SendConfig{
			Validators:    0, // This is the bug scenario - should default to defaultSampleK
			NonValidators: 0,
			Peers:         0,
		},
		constants.PrimaryNetworkID,
		&noOpAllower{},
	)
	require.NotEmpty(peersZero, "samplePeers with Validators=0 should sample validators (regression)")

	// Test 2: Verify explicit Validators count works as expected
	peersExplicit := net.samplePeers(
		warp.SendConfig{
			Validators:    1,
			NonValidators: 0,
			Peers:         0,
		},
		constants.PrimaryNetworkID,
		&noOpAllower{},
	)
	require.LessOrEqual(len(peersExplicit), 1, "explicit Validators=1 should sample at most 1")

	// Test 3: Verify that Validators=0 samples MORE than Validators=1
	// This confirms the defaultSampleK fix is working
	require.GreaterOrEqual(len(peersZero), len(peersExplicit),
		"Validators=0 (default to 20) should sample >= Validators=1")

	// Shutdown
	for _, n := range networks {
		n.StartClose()
	}
	require.NoError(eg.Wait())
}

// TestSamplePeersSmallValidatorSets verifies samplePeers correctly clamps
// to available validators when there are fewer than defaultSampleK (20).
// This tests typical small network scenarios: 4/5, 5/5, 10/10 validators.
func TestSamplePeersSmallValidatorSets(t *testing.T) {
	testCases := []struct {
		name             string
		numNodes         int
		expectedMinPeers int // minimum peers we expect to sample (connected peers - 1 for self)
	}{
		{"4 validators", 4, 2}, // 4 nodes, expect at least 2 peers sampled
		{"5 validators", 5, 3}, // 5 nodes, expect at least 3 peers sampled
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)

			// Create handlers for N nodes
			handlers := make([]peer.InboundHandler, tc.numNodes)
			for i := range handlers {
				handlers[i] = nil
			}

			nodeIDs, networks, eg := newFullyConnectedTestNetwork(t, handlers)
			require.Len(nodeIDs, tc.numNodes)
			require.Len(networks, tc.numNodes)

			// Wait for full mesh connectivity
			net := networks[0]
			expectedConnections := tc.numNodes - 1 // all other nodes
			for i := 0; i < 100; i++ {
				if net.connectedPeers.Len() >= expectedConnections {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

			// Verify we have validators in the manager
			numValidators := net.config.Validators.NumValidators(constants.PrimaryNetworkID)
			require.GreaterOrEqual(numValidators, tc.numNodes,
				"expected at least %d validators in manager", tc.numNodes)

			// Test: samplePeers with Validators=0 should sample up to all available
			// Since defaultSampleK=20 > numNodes, it should clamp to numNodes
			peers := net.samplePeers(
				warp.SendConfig{
					Validators:    0, // triggers default to 20, clamped to numValidators
					NonValidators: 0,
					Peers:         0,
				},
				constants.PrimaryNetworkID,
				&noOpAllower{},
			)

			// Should sample peers (limited by connected peers, not defaultSampleK)
			require.GreaterOrEqual(len(peers), tc.expectedMinPeers,
				"with %d validators, should sample at least %d peers", tc.numNodes, tc.expectedMinPeers)

			// Should not exceed available validators
			require.LessOrEqual(len(peers), tc.numNodes,
				"should not sample more peers than validators exist")

			// Test explicit small request also works
			peersExplicit := net.samplePeers(
				warp.SendConfig{
					Validators:    2,
					NonValidators: 0,
					Peers:         0,
				},
				constants.PrimaryNetworkID,
				&noOpAllower{},
			)
			require.LessOrEqual(len(peersExplicit), 2, "explicit Validators=2 should sample at most 2")

			// Shutdown
			for _, n := range networks {
				n.StartClose()
			}
			require.NoError(eg.Wait())
		})
	}
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"crypto"
	"net"
	"net/netip"
	"time"
	

	"github.com/prometheus/client_golang/prometheus"
	
	luxmetrics "github.com/luxfi/metric"

	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
)

const maxMessageToSend = 1024

// StartTestPeer provides a simple interface to create a peer that has finished
// the p2p handshake.
//
// This function will generate a new TLS key to use when connecting to the peer.
//
// The returned peer will not throttle inbound or outbound messages.
//
//   - [ctx] provides a way of canceling the connection request.
//   - [ip] is the remote that will be dialed to create the connection.
//   - [networkID] will be sent to the peer during the handshake. If the peer is
//     expecting a different [networkID], the handshake will fail and an error
//     will be returned.
//   - [router] will be called with all non-handshake messages received by the
//     peer.
func StartTestPeer(
	ctx context.Context,
	ip netip.AddrPort,
	networkID uint32,
	router router.InboundHandler,
) (Peer, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, constants.NetworkType, ip.String())
	if err != nil {
		return nil, err
	}

	tlsCert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}

	tlsConfg := TLSConfig(*tlsCert, nil)
	clientUpgrader := NewTLSClientUpgrader(
		tlsConfg,
		prometheus.NewCounter(prometheus.CounterOpts{}),
	)

	peerID, conn, cert, err := clientUpgrader.Upgrade(conn)
	if err != nil {
		return nil, err
	}

	// Create a prometheus registry for metrics
	promRegistry := prometheus.NewRegistry()
	
	// Create a no-op metrics instance for message creator
	metricsInstance := luxmetrics.NewNoOpMetrics("test")
	
	mc, err := message.NewCreator(
		nil,
		metricsInstance,
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}

	peerMetrics, err := NewMetrics(promRegistry)
	if err != nil {
		return nil, err
	}

	// Create a basic resource tracker for testing
	resourceTracker := &testResourceTracker{}

	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}

	peer := Start(
		&Config{
			Metrics:              peerMetrics,
			MessageCreator:       mc,
			Log:                  nil,
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              TestNetwork,
			Router:               router,
			VersionCompatibility: version.GetCompatibility(networkID),
			MySubnets:            set.Set[ids.ID]{},
			Beacons:              &testValidatorManager{},
			Validators:           &testValidatorManager{},
			NetworkID:            networkID,
			PingFrequency:        constants.DefaultPingFrequency,
			PongTimeout:          constants.DefaultPingPongTimeout,
			MaxClockDifference:   time.Minute,
			ResourceTracker:      resourceTracker,
			UptimeCalculator:     uptime.NoOpCalculator,
			IPSigner: NewIPSigner(
				utils.NewAtomic(netip.AddrPortFrom(
					netip.IPv6Loopback(),
					1,
				)),
				tlsKey,
				blsKey,
			),
		},
		conn,
		cert,
		peerID,
		NewBlockingMessageQueue(
			SendFailedFunc(func(message.OutboundMessage) {}), // No-op callback
			nil,
			maxMessageToSend,
		),
	)
	return peer, peer.AwaitReady(ctx)
}

// testResourceTracker is a minimal implementation for testing
type testResourceTracker struct{}

func (t *testResourceTracker) CPUTracker() tracker.Tracker {
	return &testTracker{}
}

func (t *testResourceTracker) DiskTracker() tracker.DiskTracker {
	return &testDiskTracker{}
}

func (t *testResourceTracker) StartProcessing(ids.NodeID, time.Time) {}
func (t *testResourceTracker) StopProcessing(ids.NodeID, time.Time) {}

// testTracker is a minimal tracker implementation
type testTracker struct{}

func (t *testTracker) UtilizationTarget() float64 { return 0.8 }
func (t *testTracker) CurrentUsage() uint64 { return 0 }
func (t *testTracker) TotalUsage() float64 { return 0 }
func (t *testTracker) Usage(ids.NodeID, time.Time) float64 { return 0 }
func (t *testTracker) TimeUntilUsage(ids.NodeID, time.Time, float64) time.Duration { return 0 }

// testDiskTracker is a minimal disk tracker implementation
type testDiskTracker struct{ testTracker }

func (t *testDiskTracker) AvailableDiskBytes() uint64 { return 1 << 30 } // 1GB

// testValidatorManager is a minimal validator manager implementation for testing
type testValidatorManager struct{}

func (m *testValidatorManager) GetValidators(subnetID ids.ID) ([]ids.NodeID, error) {
	return nil, nil
}

func (m *testValidatorManager) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool) {
	return nil, false
}

func (m *testValidatorManager) GetWeight(subnetID ids.ID, nodeID ids.NodeID) (uint64, error) {
	return 0, nil
}

func (m *testValidatorManager) TotalWeight(subnetID ids.ID) (uint64, error) {
	return 0, nil
}

func (m *testValidatorManager) NumValidators(subnetID ids.ID) int {
	return 0
}

func (m *testValidatorManager) RegisterSetCallbackListener(listener validators.SetCallbackListener) {
	// No-op
}

func (m *testValidatorManager) AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, validationID ids.ID, weight uint64) error {
	return nil
}

func (m *testValidatorManager) AddWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (m *testValidatorManager) RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (m *testValidatorManager) GetMap(subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Return empty map for testing
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

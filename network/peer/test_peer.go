// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"crypto"
	"net"
	"net/netip"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/metric"

	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/consensus/validator/uptime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
	"github.com/luxfi/utils"
	"github.com/luxfi/compress"
)

const maxMessageToSend = 1024

// testNoOpResourceManager implements tracker.ResourceManager for testing
type testNoOpResourceManager struct{}

func (*testNoOpResourceManager) CPUUsage() float64  { return 0 }
func (*testNoOpResourceManager) DiskUsage() float64 { return 0 }
func (*testNoOpResourceManager) Shutdown()          {}

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
	router InboundHandler,
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
		metric.NewCounter(metric.CounterOpts{}),
	)

	peerID, conn, cert, err := clientUpgrader.Upgrade(conn)
	if err != nil {
		return nil, err
	}

	// Create a metrics registry
	reg := metric.NewRegistry()

	mc, err := message.NewCreator(
		reg,
		compress.Type(constants.DefaultNetworkCompressionType),
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}

	peerMetrics, err := NewMetrics(reg)
	if err != nil {
		return nil, err
	}

	// Create a no-op resource manager for testing
	noOpManager := &testNoOpResourceManager{}
	resourceTracker, err := tracker.NewResourceTracker(
		noOpManager,
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}

	tlsKey := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	if err != nil {
		return nil, err
	}

	peer := Start(
		&Config{
			Metrics:              peerMetrics,
			MessageCreator:       mc,
			Log:                  log.Noop(),
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              TestNetwork,
			Router:               router,
			VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
			MyChains:             make(set.Set[ids.ID]),
			Beacons:              &testValidatorManager{},
			Validators:           &testValidatorManager{},
			NetworkID:            networkID,
			PingFrequency:        constants.DefaultPingFrequency,
			PongTimeout:          constants.DefaultPingPongTimeout,
			MaxClockDifference:   time.Minute,
			ResourceTracker:      resourceTracker,
			UptimeCalculator:     uptime.NoOpCalculator{},
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
			nil,
			log.Noop(),
			maxMessageToSend,
		),
		false,
	)
	return peer, peer.AwaitReady(ctx)
}

// testResourceTracker is a minimal implementation for testing
type testResourceTracker struct{}

func (t *testResourceTracker) CPUTracker() tracker.Tracker {
	return &testCPUTracker{}
}

func (t *testResourceTracker) DiskTracker() tracker.Tracker {
	return &testDiskTracker{}
}

func (t *testResourceTracker) StartProcessing(ids.NodeID, time.Time) {}
func (t *testResourceTracker) StopProcessing(ids.NodeID, time.Time)  {}

// testCPUTracker is a minimal CPU tracker implementation
type testCPUTracker struct{}

func (t *testCPUTracker) Usage(ids.NodeID, time.Time) float64                         { return 0 }
func (t *testCPUTracker) TimeUntilUsage(ids.NodeID, time.Time, float64) time.Duration { return 0 }
func (t *testCPUTracker) TotalUsage() float64                                         { return 0 }

// testTracker is a minimal tracker implementation
type testTracker struct{}

func (t *testTracker) UtilizationTarget() float64                                  { return 0.8 }
func (t *testTracker) CurrentUsage() uint64                                        { return 0 }
func (t *testTracker) TotalUsage() float64                                         { return 0 }
func (t *testTracker) Usage(ids.NodeID, time.Time) float64                         { return 0 }
func (t *testTracker) TimeUntilUsage(ids.NodeID, time.Time, float64) time.Duration { return 0 }

// testDiskTracker is a minimal disk tracker implementation
type testDiskTracker struct{ testTracker }

func (t *testDiskTracker) AvailableDiskBytes() uint64 { return 1 << 30 } // 1GB

// testValidatorManager is a minimal validator manager implementation for testing
type testValidatorManager struct{}

func (m *testValidatorManager) GetValidators(netID ids.ID) (validators.Set, error) {
	return nil, nil
}

func (m *testValidatorManager) GetValidatorIDs(netID ids.ID) []ids.NodeID {
	return nil
}

func (m *testValidatorManager) GetValidator(netID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	return nil, false
}

func (m *testValidatorManager) GetWeight(netID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (m *testValidatorManager) GetLight(netID ids.ID, nodeID ids.NodeID) uint64 {
	return 0
}

func (m *testValidatorManager) TotalWeight(netID ids.ID) (uint64, error) {
	return 0, nil
}

func (m *testValidatorManager) TotalLight(netID ids.ID) (uint64, error) {
	return 0, nil
}

func (m *testValidatorManager) NumValidators(netID ids.ID) int {
	return 0
}

func (m *testValidatorManager) RegisterSetCallbackListener(netID ids.ID, listener validators.SetCallbackListener) {
	// No-op
}

func (m *testValidatorManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
	// No-op for testing
}

func (m *testValidatorManager) AddStaker(netID ids.ID, nodeID ids.NodeID, publicKey []byte, validationID ids.ID, light uint64) error {
	return nil
}

func (m *testValidatorManager) AddWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (m *testValidatorManager) RemoveWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return nil
}

func (m *testValidatorManager) GetMap(netID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	// Return empty map for testing
	return make(map[ids.NodeID]*validators.GetValidatorOutput)
}

func (m *testValidatorManager) SubsetWeight(netID ids.ID, validatorIDs set.Set[ids.NodeID]) (uint64, error) {
	return 0, nil
}

func (m *testValidatorManager) Sample(netID ids.ID, n int) ([]ids.NodeID, error) {
	// Return empty sample for testing
	return nil, nil
}

func (m *testValidatorManager) NumNets() int {
	return 0
}

func (m *testValidatorManager) Count(netID ids.ID) int {
	return 0
}

func (m *testValidatorManager) String() string {
	return "testValidatorManager"
}

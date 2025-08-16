// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"errors"
	"math"
	"net"
	"net/netip"
	"runtime"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	luxmetrics "github.com/luxfi/metric"

	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/units"
)

var (
	errClosed = errors.New("closed")

	_ net.Listener    = (*noopListener)(nil)
	_ subnets.Allower = (*nodeIDConnector)(nil)
)

type noopListener struct {
	once   sync.Once
	closed chan struct{}
}

func newNoopListener() net.Listener {
	return &noopListener{
		closed: make(chan struct{}),
	}
}

func (l *noopListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, errClosed
}

func (l *noopListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (*noopListener) Addr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IPv4zero,
		Port: 1,
	}
}

func NewTestNetwork(
	log log.Logger,
	networkID uint32,
	currentValidators validators.Manager,
	trackedSubnets set.Set[ids.ID],
	router router.ExternalHandler,
) (Network, error) {
	m := luxmetrics.NewNoOpMetrics("test")
	msgCreator, err := message.NewCreator(
		log,
		m,
		constants.DefaultNetworkCompressionType,
		constants.DefaultNetworkMaximumInboundTimeout,
	)
	if err != nil {
		return nil, err
	}

	tlsCert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}

	blsKey, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}

	// TODO actually monitor usage
	// TestNetwork doesn't use disk so we don't need to track it, but we should
	// still have guardrails around cpu/memory usage.
	promRegistry := prometheus.NewRegistry()

	resourceTracker, err := tracker.NewResourceTracker(
		promRegistry,
		&noOpResourceManager{},
		constants.DefaultHealthCheckAveragerHalflife,
	)
	if err != nil {
		return nil, err
	}

	return NewNetwork(
		&Config{
			HealthConfig: HealthConfig{
				Enabled:                      true,
				MinConnectedPeers:            constants.DefaultNetworkHealthMinPeers,
				MaxTimeSinceMsgReceived:      constants.DefaultNetworkHealthMaxTimeSinceMsgReceived,
				MaxTimeSinceMsgSent:          constants.DefaultNetworkHealthMaxTimeSinceMsgSent,
				MaxPortionSendQueueBytesFull: constants.DefaultNetworkHealthMaxPortionSendQueueFill,
				MaxSendFailRate:              constants.DefaultNetworkHealthMaxSendFailRate,
				SendFailRateHalflife:         constants.DefaultHealthCheckAveragerHalflife,
			},
			PeerListGossipConfig: PeerListGossipConfig{
				PeerListNumValidatorIPs: constants.DefaultNetworkPeerListNumValidatorIPs,
				PeerListPullGossipFreq:  constants.DefaultNetworkPeerListPullGossipFreq,
				PeerListBloomResetFreq:  constants.DefaultNetworkPeerListBloomResetFreq,
			},
			TimeoutConfig: TimeoutConfig{
				PingPongTimeout:      constants.DefaultPingPongTimeout,
				ReadHandshakeTimeout: constants.DefaultNetworkReadHandshakeTimeout,
			},
			DelayConfig: DelayConfig{
				InitialReconnectDelay: constants.DefaultNetworkInitialReconnectDelay,
				MaxReconnectDelay:     constants.DefaultNetworkMaxReconnectDelay,
			},
			ThrottlerConfig: ThrottlerConfig{
				InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
					UpgradeCooldown:        constants.DefaultInboundConnUpgradeThrottlerCooldown,
					MaxRecentConnsUpgraded: int(math.Ceil(constants.DefaultInboundThrottlerMaxConnsPerSec * constants.DefaultInboundConnUpgradeThrottlerCooldown.Seconds())),
				},
				InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
					MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
						VdrAllocSize:        constants.DefaultInboundThrottlerVdrAllocSize,
						AtLargeAllocSize:    constants.DefaultInboundThrottlerAtLargeAllocSize,
						NodeMaxAtLargeBytes: constants.DefaultInboundThrottlerNodeMaxAtLargeBytes,
					},
					BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
						RefillRate:   constants.DefaultInboundThrottlerBandwidthRefillRate,
						MaxBurstSize: constants.DefaultInboundThrottlerBandwidthMaxBurstSize,
					},
					CPUThrottlerConfig: throttling.SystemThrottlerConfig{
						MaxRecheckDelay: constants.DefaultInboundThrottlerCPUMaxRecheckDelay,
					},
					DiskThrottlerConfig: throttling.SystemThrottlerConfig{
						MaxRecheckDelay: constants.DefaultInboundThrottlerDiskMaxRecheckDelay,
					},
					MaxProcessingMsgsPerNode: constants.DefaultInboundThrottlerMaxProcessingMsgsPerNode,
				},
				OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
					VdrAllocSize:        constants.DefaultOutboundThrottlerVdrAllocSize,
					AtLargeAllocSize:    constants.DefaultOutboundThrottlerAtLargeAllocSize,
					NodeMaxAtLargeBytes: constants.DefaultOutboundThrottlerNodeMaxAtLargeBytes,
				},
				MaxInboundConnsPerSec: constants.DefaultInboundThrottlerMaxConnsPerSec,
			},
			ProxyEnabled:           constants.DefaultNetworkTCPProxyEnabled,
			ProxyReadHeaderTimeout: constants.DefaultNetworkTCPProxyReadTimeout,
			DialerConfig: dialer.Config{
				ThrottleRps:       constants.DefaultOutboundConnectionThrottlingRps,
				ConnectionTimeout: constants.DefaultOutboundConnectionTimeout,
			},
			TLSConfig: peer.TLSConfig(*tlsCert, nil),
			MyIPPort: utils.NewAtomic(netip.AddrPortFrom(
				netip.IPv4Unspecified(),
				1,
			)),
			NetworkID:                    networkID,
			MaxClockDifference:           constants.DefaultNetworkMaxClockDifference,
			PingFrequency:                constants.DefaultPingFrequency,
			AllowPrivateIPs:              !constants.ProductionNetworkIDs.Contains(networkID),
			CompressionType:              constants.DefaultNetworkCompressionType,
			TLSKey:                       tlsCert.PrivateKey.(crypto.Signer),
			BLSKey:                       blsKey,
			TrackedSubnets:               trackedSubnets,
			Beacons:                      validators.NewManager(),
			Validators:                   currentValidators,
			UptimeCalculator:             uptime.NoOpCalculator,
			UptimeMetricFreq:             constants.DefaultUptimeMetricFreq,
			RequireValidatorToConnect:    constants.DefaultNetworkRequireValidatorToConnect,
			MaximumInboundMessageTimeout: constants.DefaultNetworkMaximumInboundTimeout,
			PeerReadBufferSize:           constants.DefaultNetworkPeerReadBufferSize,
			PeerWriteBufferSize:          constants.DefaultNetworkPeerWriteBufferSize,
			ResourceTracker:              resourceTracker,
			CPUTargeter: tracker.NewTargeter(
				log,
				&tracker.TargeterConfig{
					VdrAlloc:           float64(runtime.NumCPU()),
					MaxNonVdrUsage:     .8 * float64(runtime.NumCPU()),
					MaxNonVdrNodeUsage: float64(runtime.NumCPU()) / 8,
				},
				currentValidators,
				resourceTracker.CPUTracker(),
			),
			DiskTargeter: tracker.NewTargeter(
				log,
				&tracker.TargeterConfig{
					VdrAlloc:           1000 * units.GiB,
					MaxNonVdrUsage:     1000 * units.GiB,
					MaxNonVdrNodeUsage: 1000 * units.GiB,
				},
				currentValidators,
				resourceTracker.DiskTracker(),
			),
		},
		msgCreator,
		promRegistry,
		log,
		newNoopListener(),
		dialer.NewDialer(
			constants.NetworkType,
			dialer.Config{
				ThrottleRps:       constants.DefaultOutboundConnectionThrottlingRps,
				ConnectionTimeout: constants.DefaultOutboundConnectionTimeout,
			},
			log,
		),
		router,
	)
}

type nodeIDConnector struct {
	nodeID ids.NodeID
}

func newNodeIDConnector(nodeID ids.NodeID) *nodeIDConnector {
	return &nodeIDConnector{nodeID: nodeID}
}

func (f *nodeIDConnector) IsAllowed(nodeID ids.NodeID, _ bool) bool {
	return nodeID == f.nodeID
}

// noOpResourceManager is a no-op resource manager for testing
type noOpResourceManager struct{}

func (n *noOpResourceManager) CPUUsage() float64             { return 0 }
func (n *noOpResourceManager) DiskUsage() (float64, float64) { return 0, 0 }
func (n *noOpResourceManager) Shutdown()                     {}
func (n *noOpResourceManager) AvailableDiskBytes() uint64    { return 1 << 30 } // 1GB
func (n *noOpResourceManager) TrackProcess(pid int)          { /* no-op */ }
func (n *noOpResourceManager) UntrackProcess(pid int)        { /* no-op */ }

// noOpMetricsFactory is a no-op metrics factory for testing
type noOpMetricsFactory struct{}

func (n *noOpMetricsFactory) New(string) luxmetrics.Metrics {
	return luxmetrics.NewNoOpMetrics("test")
}

func (n *noOpMetricsFactory) NewWithRegistry(string, luxmetrics.Registry) luxmetrics.Metrics {
	return luxmetrics.NewNoOpMetrics("test")
}

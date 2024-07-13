// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
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

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/snow/networking/router"
	"github.com/luxfi/node/snow/networking/tracker"
	"github.com/luxfi/node/snow/uptime"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/math/meter"
	"github.com/luxfi/node/utils/resource"
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
	log logging.Logger,
	networkID uint32,
	currentValidators validators.Manager,
	trackedSubnets set.Set[ids.ID],
	router router.ExternalHandler,
) (Network, error) {
	metrics := prometheus.NewRegistry()
	msgCreator, err := message.NewCreator(
		logging.NoLog{},
		metrics,
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
	resourceTracker, err := tracker.NewResourceTracker(
		metrics,
		resource.NoUsage,
		&meter.ContinuousFactory{},
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
				logging.NoLog{},
				&tracker.TargeterConfig{
					VdrAlloc:           float64(runtime.NumCPU()),
					MaxNonVdrUsage:     .8 * float64(runtime.NumCPU()),
					MaxNonVdrNodeUsage: float64(runtime.NumCPU()) / 8,
				},
				currentValidators,
				resourceTracker.CPUTracker(),
			),
			DiskTargeter: tracker.NewTargeter(
				logging.NoLog{},
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
		metrics,
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

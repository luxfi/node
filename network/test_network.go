// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
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
	"time"

	"github.com/luxfi/metric"

	consensustracker "github.com/luxfi/consensus/networking/tracker"
	nodevalidators "github.com/luxfi/validators"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/utils"
	compression "github.com/luxfi/compress"
)

var (
	errClosed = errors.New("closed")

	_ net.Listener = (*noopListener)(nil)
	_ nets.Allower = (*nodeIDConnector)(nil)
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

func NewTestNetworkConfig(
	metrics metric.Registerer,
	networkID uint32,
	currentValidators validators.Manager,
	trackedChains set.Set[ids.ID],
) (*Config, error) {
	tlsCert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}

	blsKey, err := localsigner.New()
	if err != nil {
		return nil, err
	}

	// TestNetwork doesn't use disk so resource tracking is not needed.
	return &Config{
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
		AllowPrivateIPs:              true, // Allow private IPs by default for testing
		CompressionType:              compression.Type(constants.DefaultNetworkCompressionType),
		TLSKey:                       tlsCert.PrivateKey.(crypto.Signer),
		BLSKey:                       blsKey,
		TrackedChains:                trackedChains,
		Beacons:                      nodevalidators.NewManager(),
		Validators:                   nodevalidators.NewManager(),
		UptimeCalculator:             &uptime.NoOpCalculator{},
		UptimeMetricFreq:             constants.DefaultUptimeMetricFreq,
		RequireValidatorToConnect:    constants.DefaultNetworkRequireValidatorToConnect,
		MaximumInboundMessageTimeout: constants.DefaultNetworkMaximumInboundTimeout,
		PeerReadBufferSize:           constants.DefaultNetworkPeerReadBufferSize,
		PeerWriteBufferSize:          constants.DefaultNetworkPeerWriteBufferSize,
		ResourceTracker:              &noOpConsensusResourceTracker{},
		CPUTargeter: tracker.NewTargeter(
			&tracker.TargeterConfig{
				VdrAlloc:           float64(runtime.NumCPU()),
				MaxNonVdrUsage:     .8 * float64(runtime.NumCPU()),
				MaxNonVdrNodeUsage: float64(runtime.NumCPU()) / 8,
			},
		),
		DiskTargeter: tracker.NewTargeter(
			&tracker.TargeterConfig{
				VdrAlloc:           1000 * constants.GiB,
				MaxNonVdrUsage:     1000 * constants.GiB,
				MaxNonVdrNodeUsage: 1000 * constants.GiB,
			},
		),
	}, nil
}

func NewTestNetwork(
	log log.Logger,
	registry metric.Registry,
	cfg *Config,
	router ExternalHandler,
) (Network, error) {
	msgCreator, err := message.NewCreator(
		registry,
		compression.Type(constants.DefaultNetworkCompressionType),
		constants.DefaultNetworkMaximumInboundTimeout,
	)
	if err != nil {
		return nil, err
	}

	return NewNetwork(
		cfg,
		upgrade.GetConfig(cfg.NetworkID).FortunaTime, // Must be updated for each network upgrade
		msgCreator,
		registry,
		log,
		newNoopListener(),
		dialer.NewEndpointDialer(
			constants.NetworkType,
			dialer.EndpointDialerConfig{
				Config: dialer.Config{
					ThrottleRps:       constants.DefaultOutboundConnectionThrottlingRps,
					ConnectionTimeout: constants.DefaultOutboundConnectionTimeout,
				},
				DNSConfig: dialer.DefaultDNSCacheConfig(),
			},
			log,
		),
		router,
	)
}

type nodeIDConnector struct {
	nodeID ids.NodeID
}

func (f *nodeIDConnector) IsAllowed(nodeID ids.NodeID, _ bool) bool {
	return nodeID == f.nodeID
}

// noOpConsensusResourceTracker implements consensus ResourceTracker for testing
type noOpConsensusResourceTracker struct{}

func (n *noOpConsensusResourceTracker) StartProcessing(nodeID ids.NodeID, now time.Time) {}
func (n *noOpConsensusResourceTracker) StopProcessing(nodeID ids.NodeID, now time.Time)  {}
func (n *noOpConsensusResourceTracker) CPUTracker() consensustracker.CPUTracker {
	return &noOpConsensusCPUTracker{}
}
func (n *noOpConsensusResourceTracker) DiskTracker() consensustracker.DiskTracker {
	return &noOpConsensusDiskTracker{}
}

// noOpConsensusCPUTracker implements consensus CPUTracker for testing
type noOpConsensusCPUTracker struct{}

func (n *noOpConsensusCPUTracker) Usage(nodeID ids.NodeID, now time.Time) float64 { return 0 }
func (n *noOpConsensusCPUTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	return 0
}
func (n *noOpConsensusCPUTracker) TotalUsage() float64 { return 0 }

// noOpConsensusDiskTracker implements consensus DiskTracker for testing
type noOpConsensusDiskTracker struct{}

func (n *noOpConsensusDiskTracker) Usage(nodeID ids.NodeID, now time.Time) float64 { return 0 }
func (n *noOpConsensusDiskTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	return 0
}
func (n *noOpConsensusDiskTracker) TotalUsage() float64 { return 0 }


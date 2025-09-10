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
	"time"

	"github.com/prometheus/client_golang/prometheus"

	luxmetrics "github.com/luxfi/metric"

	"github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/consensus/uptime"
	consensusset "github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/staking"
	subnets "github.com/luxfi/node/nets"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/math/set"
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
	router ExternalHandler,
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

	resourceTracker := &noOpResourceTracker{}

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
			Beacons:                      &noOpValidatorsManager{},
			Validators:                   currentValidators,
			UptimeCalculator:             &uptime.NoOpCalculator{},
			UptimeMetricFreq:             constants.DefaultUptimeMetricFreq,
			RequireValidatorToConnect:    constants.DefaultNetworkRequireValidatorToConnect,
			MaximumInboundMessageTimeout: constants.DefaultNetworkMaximumInboundTimeout,
			PeerReadBufferSize:           constants.DefaultNetworkPeerReadBufferSize,
			PeerWriteBufferSize:          constants.DefaultNetworkPeerWriteBufferSize,
			ResourceTracker:              resourceTracker,
			CPUTargeter:  &noOpTargeter{target: uint64(runtime.NumCPU())},
			DiskTargeter: &noOpTargeter{target: 1000 * units.GiB},
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

func (n *noOpResourceManager) CPUUsage() float64  { return 0 }
func (n *noOpResourceManager) DiskUsage() float64 { return 0 }
func (n *noOpResourceManager) Shutdown()          {}

// noOpTargeter is a no-op targeter for testing
type noOpTargeter struct {
	target uint64
}

func (n *noOpTargeter) TargetUsage() uint64 { return n.target }

// noOpResourceTracker is a no-op resource tracker for testing that implements consensus ResourceTracker
type noOpResourceTracker struct{}

func (n *noOpResourceTracker) StartProcessing(nodeID ids.NodeID, time time.Time) {}
func (n *noOpResourceTracker) StopProcessing(nodeID ids.NodeID, time time.Time)  {}
func (n *noOpResourceTracker) CPUTracker() tracker.CPUTracker                    { return &noOpCPUTracker{} }
func (n *noOpResourceTracker) DiskTracker() tracker.DiskTracker                  { return &noOpDiskTracker{} }

// noOpCPUTracker is a no-op CPU tracker
type noOpCPUTracker struct{}

func (n *noOpCPUTracker) Usage(nodeID ids.NodeID, now time.Time) float64 { return 0 }
func (n *noOpCPUTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	return time.Duration(0)
}
func (n *noOpCPUTracker) TotalUsage() float64 { return 0 }

// noOpDiskTracker is a no-op disk tracker  
type noOpDiskTracker struct{}

func (n *noOpDiskTracker) Usage(nodeID ids.NodeID, now time.Time) float64 { return 0 }
func (n *noOpDiskTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	return time.Duration(0)
}
func (n *noOpDiskTracker) TotalUsage() float64 { return 0 }

// noOpValidatorsManager is a no-op validators manager for testing
type noOpValidatorsManager struct{}

func (n *noOpValidatorsManager) GetValidators(netID ids.ID) (validators.Set, error) {
	return &noOpValidatorSet{}, nil
}
func (n *noOpValidatorsManager) GetValidator(netID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	return nil, false
}
func (n *noOpValidatorsManager) GetLight(netID ids.ID, nodeID ids.NodeID) uint64 { return 0 }
func (n *noOpValidatorsManager) GetWeight(netID ids.ID, nodeID ids.NodeID) uint64 { return 0 }
func (n *noOpValidatorsManager) TotalLight(netID ids.ID) (uint64, error) { return 0, nil }
func (n *noOpValidatorsManager) TotalWeight(netID ids.ID) (uint64, error) { return 0, nil }
func (n *noOpValidatorsManager) AddStaker(netID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error { return nil }
func (n *noOpValidatorsManager) AddWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error { return nil }
func (n *noOpValidatorsManager) RemoveWeight(netID ids.ID, nodeID ids.NodeID, weight uint64) error { return nil }
func (n *noOpValidatorsManager) GetMap(netID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput { return nil }
func (n *noOpValidatorsManager) GetValidatorIDs(netID ids.ID) []ids.NodeID { return nil }
func (n *noOpValidatorsManager) NumValidators(netID ids.ID) int { return 0 }
func (n *noOpValidatorsManager) NumSubnets() int { return 0 }
func (n *noOpValidatorsManager) SubsetWeight(netID ids.ID, nodeIDs consensusset.Set[ids.NodeID]) (uint64, error) { return 0, nil }
func (n *noOpValidatorsManager) Sample(netID ids.ID, size int) ([]ids.NodeID, error) { return nil, nil }
func (n *noOpValidatorsManager) Count(netID ids.ID) int { return 0 }
func (n *noOpValidatorsManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {}
func (n *noOpValidatorsManager) RegisterSetCallbackListener(netID ids.ID, listener validators.SetCallbackListener) {}

// noOpValidatorSet is a no-op validator set for testing
type noOpValidatorSet struct{}

func (n *noOpValidatorSet) Has(ids.NodeID) bool                     { return false }
func (n *noOpValidatorSet) Len() int                                 { return 0 }
func (n *noOpValidatorSet) List() []validators.Validator             { return nil }
func (n *noOpValidatorSet) Light() uint64                            { return 0 }
func (n *noOpValidatorSet) Sample(size int) ([]ids.NodeID, error)   { return nil, nil }

// noOpMetricsFactory is a no-op metrics factory for testing
type noOpMetricsFactory struct{}

func (n *noOpMetricsFactory) New(string) luxmetrics.Metrics {
	return luxmetrics.NewNoOpMetrics("test")
}

func (n *noOpMetricsFactory) NewWithRegistry(string, luxmetrics.Registry) luxmetrics.Metrics {
	return luxmetrics.NewNoOpMetrics("test")
}

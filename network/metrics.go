// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"sync"
	"time"

	"github.com/luxfi/metric"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/network/peer"
)

type metricsImpl struct {
	// trackedSubnets does not include the primary network ID
	trackedSubnets set.Set[ids.ID]

	numTracked                      metric.Gauge
	numPeers                        metric.Gauge
	numSubnetPeers                  metric.GaugeVec
	timeSinceLastMsgSent            metric.Gauge
	timeSinceLastMsgReceived        metric.Gauge
	sendFailRate                    metric.Gauge
	connected                       metric.Counter
	disconnected                    metric.Counter
	acceptFailed                    metric.Counter
	inboundConnRateLimited          metric.Counter
	inboundConnAllowed              metric.Counter
	tlsConnRejected                 metric.Counter
	numUselessPeerListBytes         metric.Counter
	nodeUptimeWeightedAverage       metric.Gauge
	nodeUptimeRewardingStake        metric.Gauge
	nodeSubnetUptimeWeightedAverage metric.GaugeVec
	nodeSubnetUptimeRewardingStake  metric.GaugeVec
	peerConnectedLifetimeAverage    metric.Gauge

	lock                       sync.RWMutex
	peerConnectedStartTimes    map[ids.NodeID]float64
	peerConnectedStartTimesSum float64
}

func newMetrics(
	registerer metric.Registerer,
	trackedSubnets set.Set[ids.ID],
) (*metricsImpl, error) {
	m := &metricsImpl{
		trackedSubnets: trackedSubnets,
		numPeers: metric.NewGauge(metric.GaugeOpts{
			Name: "peers",
			Help: "Number of network peers",
		}),
		numTracked: metric.NewGauge(metric.GaugeOpts{
			Name: "tracked",
			Help: "Number of currently tracked IPs attempting to be connected to",
		}),
		numSubnetPeers: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "peers_subnet",
				Help: "Number of peers that are validating a particular subnet",
			},
			[]string{"netID"},
		),
		timeSinceLastMsgReceived: metric.NewGauge(metric.GaugeOpts{
			Name: "time_since_last_msg_received",
			Help: "Time (in ns) since the last msg was received",
		}),
		timeSinceLastMsgSent: metric.NewGauge(metric.GaugeOpts{
			Name: "time_since_last_msg_sent",
			Help: "Time (in ns) since the last msg was sent",
		}),
		sendFailRate: metric.NewGauge(metric.GaugeOpts{
			Name: "send_fail_rate",
			Help: "Portion of messages that recently failed to be sent over the network",
		}),
		connected: metric.NewCounter(metric.CounterOpts{
			Name: "times_connected",
			Help: "Times this node successfully completed a handshake with a peer",
		}),
		disconnected: metric.NewCounter(metric.CounterOpts{
			Name: "times_disconnected",
			Help: "Times this node disconnected from a peer it had completed a handshake with",
		}),
		acceptFailed: metric.NewCounter(metric.CounterOpts{
			Name: "accept_failed",
			Help: "Times this node's listener failed to accept an inbound connection",
		}),
		inboundConnAllowed: metric.NewCounter(metric.CounterOpts{
			Name: "inbound_conn_throttler_allowed",
			Help: "Times this node allowed (attempted to upgrade) an inbound connection",
		}),
		tlsConnRejected: metric.NewCounter(metric.CounterOpts{
			Name: "tls_conn_rejected",
			Help: "Times this node rejected a connection due to an unsupported TLS certificate",
		}),
		numUselessPeerListBytes: metric.NewCounter(metric.CounterOpts{
			Name: "num_useless_peerlist_bytes",
			Help: "Amount of useless bytes (i.e. information about nodes we already knew/don't want to connect to) received in PeerList messages",
		}),
		inboundConnRateLimited: metric.NewCounter(metric.CounterOpts{
			Name: "inbound_conn_throttler_rate_limited",
			Help: "Times this node rejected an inbound connection due to rate-limiting",
		}),
		nodeUptimeWeightedAverage: metric.NewGauge(metric.GaugeOpts{
			Name: "node_uptime_weighted_average",
			Help: "This node's uptime average weighted by observing peer stakes",
		}),
		nodeUptimeRewardingStake: metric.NewGauge(metric.GaugeOpts{
			Name: "node_uptime_rewarding_stake",
			Help: "The percentage of total stake which thinks this node is eligible for rewards",
		}),
		nodeSubnetUptimeWeightedAverage: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "node_subnet_uptime_weighted_average",
				Help: "This node's net uptime averages weighted by observing net peer stakes",
			},
			[]string{"netID"},
		),
		nodeSubnetUptimeRewardingStake: metric.NewGaugeVec(
			metric.GaugeOpts{
				Name: "node_subnet_uptime_rewarding_stake",
				Help: "The percentage of subnet's total stake which thinks this node is eligible for subnet's rewards",
			},
			[]string{"netID"},
		),
		peerConnectedLifetimeAverage: metric.NewGauge(
			metric.GaugeOpts{
				Name: "peer_connected_duration_average",
				Help: "The average duration of all peer connections in nanoseconds",
			},
		),
		peerConnectedStartTimes: make(map[ids.NodeID]float64),
	}

	err := errors.Join(
		registerer.Register(m.numTracked),
		registerer.Register(m.numPeers),
		registerer.Register(m.numSubnetPeers),
		registerer.Register(m.timeSinceLastMsgReceived),
		registerer.Register(m.timeSinceLastMsgSent),
		registerer.Register(m.sendFailRate),
		registerer.Register(m.connected),
		registerer.Register(m.disconnected),
		registerer.Register(m.acceptFailed),
		registerer.Register(m.inboundConnAllowed),
		registerer.Register(m.tlsConnRejected),
		registerer.Register(m.numUselessPeerListBytes),
		registerer.Register(m.inboundConnRateLimited),
		registerer.Register(m.nodeUptimeWeightedAverage),
		registerer.Register(m.nodeUptimeRewardingStake),
		registerer.Register(m.nodeSubnetUptimeWeightedAverage),
		registerer.Register(m.nodeSubnetUptimeRewardingStake),
		registerer.Register(m.peerConnectedLifetimeAverage),
	)

	// init net tracker metrics with tracked subnets
	for netID := range trackedSubnets {
		// initialize to 0
		netIDStr := netID.String()
		m.numSubnetPeers.WithLabelValues(netIDStr).Set(0)
		m.nodeSubnetUptimeWeightedAverage.WithLabelValues(netIDStr).Set(0)
		m.nodeSubnetUptimeRewardingStake.WithLabelValues(netIDStr).Set(0)
	}

	return m, err
}

func (m *metricsImpl) markConnected(peer peer.Peer) {
	m.numPeers.Inc()
	m.connected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for netID := range m.trackedSubnets {
		if trackedSubnets.Contains(netID) {
			m.numSubnetPeers.WithLabelValues(netID.String()).Inc()
		}
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	now := float64(time.Now().UnixNano())
	m.peerConnectedStartTimes[peer.ID()] = now
	m.peerConnectedStartTimesSum += now
}

func (m *metricsImpl) markDisconnected(peer peer.Peer) {
	m.numPeers.Dec()
	m.disconnected.Inc()

	trackedSubnets := peer.TrackedSubnets()
	for netID := range m.trackedSubnets {
		if trackedSubnets.Contains(netID) {
			m.numSubnetPeers.WithLabelValues(netID.String()).Dec()
		}
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	peerID := peer.ID()
	start := m.peerConnectedStartTimes[peerID]
	m.peerConnectedStartTimesSum -= start

	delete(m.peerConnectedStartTimes, peerID)
}

func (m *metricsImpl) updatePeerConnectionLifetimeMetrics() {
	m.lock.RLock()
	defer m.lock.RUnlock()

	avg := float64(0)
	if n := len(m.peerConnectedStartTimes); n > 0 {
		avgStartTime := m.peerConnectedStartTimesSum / float64(n)
		avg = float64(time.Now().UnixNano()) - avgStartTime
	}

	m.peerConnectedLifetimeAverage.Set(avg)
}

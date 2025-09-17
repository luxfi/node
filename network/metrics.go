// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/prometheus/client_golang/prometheus"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/metric"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/math/set"
)

type metricsImpl struct {
	// trackedSubnets does not include the primary network ID
	trackedSubnets set.Set[ids.ID]

	numTracked                      metrics.Gauge
	numPeers                        metrics.Gauge
	numSubnetPeers                  metrics.GaugeVec
	timeSinceLastMsgSent            metrics.Gauge
	timeSinceLastMsgReceived        metrics.Gauge
	sendFailRate                    metrics.Gauge
	connected                       metrics.Counter
	disconnected                    metrics.Counter
	acceptFailed                    metrics.Counter
	inboundConnRateLimited          metrics.Counter
	inboundConnAllowed              metrics.Counter
	tlsConnRejected                 metrics.Counter
	numUselessPeerListBytes         metrics.Counter
	nodeUptimeWeightedAverage       metrics.Gauge
	nodeUptimeRewardingStake        metrics.Gauge
	nodeSubnetUptimeWeightedAverage metrics.GaugeVec
	nodeSubnetUptimeRewardingStake  metrics.GaugeVec
	peerConnectedLifetimeAverage    metrics.Gauge

	lock                       sync.RWMutex
	peerConnectedStartTimes    map[ids.NodeID]float64
	peerConnectedStartTimesSum float64
}

func newMetrics(
	registerer metrics.Registerer,
	trackedSubnets set.Set[ids.ID],
) (*metricsImpl, error) {
	m := &metricsImpl{
		trackedSubnets: trackedSubnets,
		numPeers: metrics.NewGauge(metrics.GaugeOpts{
			Name: "peers",
			Help: "Number of network peers",
		}),
		numTracked: metrics.NewGauge(metrics.GaugeOpts{
			Name: "tracked",
			Help: "Number of currently tracked IPs attempting to be connected to",
		}),
		numSubnetPeers: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Name: "peers_subnet",
				Help: "Number of peers that are validating a particular subnet",
			},
			[]string{"netID"},
		),
		timeSinceLastMsgReceived: metrics.NewGauge(metrics.GaugeOpts{
			Name: "time_since_last_msg_received",
			Help: "Time (in ns) since the last msg was received",
		}),
		timeSinceLastMsgSent: metrics.NewGauge(metrics.GaugeOpts{
			Name: "time_since_last_msg_sent",
			Help: "Time (in ns) since the last msg was sent",
		}),
		sendFailRate: metrics.NewGauge(metrics.GaugeOpts{
			Name: "send_fail_rate",
			Help: "Portion of messages that recently failed to be sent over the network",
		}),
		connected: metrics.NewCounter(metrics.CounterOpts{
			Name: "times_connected",
			Help: "Times this node successfully completed a handshake with a peer",
		}),
		disconnected: metrics.NewCounter(metrics.CounterOpts{
			Name: "times_disconnected",
			Help: "Times this node disconnected from a peer it had completed a handshake with",
		}),
		acceptFailed: metrics.NewCounter(metrics.CounterOpts{
			Name: "accept_failed",
			Help: "Times this node's listener failed to accept an inbound connection",
		}),
		inboundConnAllowed: metrics.NewCounter(metrics.CounterOpts{
			Name: "inbound_conn_throttler_allowed",
			Help: "Times this node allowed (attempted to upgrade) an inbound connection",
		}),
		tlsConnRejected: metrics.NewCounter(metrics.CounterOpts{
			Name: "tls_conn_rejected",
			Help: "Times this node rejected a connection due to an unsupported TLS certificate",
		}),
		numUselessPeerListBytes: metrics.NewCounter(metrics.CounterOpts{
			Name: "num_useless_peerlist_bytes",
			Help: "Amount of useless bytes (i.e. information about nodes we already knew/don't want to connect to) received in PeerList messages",
		}),
		inboundConnRateLimited: metrics.NewCounter(metrics.CounterOpts{
			Name: "inbound_conn_throttler_rate_limited",
			Help: "Times this node rejected an inbound connection due to rate-limiting",
		}),
		nodeUptimeWeightedAverage: metrics.NewGauge(metrics.GaugeOpts{
			Name: "node_uptime_weighted_average",
			Help: "This node's uptime average weighted by observing peer stakes",
		}),
		nodeUptimeRewardingStake: metrics.NewGauge(metrics.GaugeOpts{
			Name: "node_uptime_rewarding_stake",
			Help: "The percentage of total stake which thinks this node is eligible for rewards",
		}),
		nodeSubnetUptimeWeightedAverage: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Name: "node_subnet_uptime_weighted_average",
				Help: "This node's net uptime averages weighted by observing net peer stakes",
			},
			[]string{"netID"},
		),
		nodeSubnetUptimeRewardingStake: metrics.NewGaugeVec(
			metrics.GaugeOpts{
				Name: "node_subnet_uptime_rewarding_stake",
				Help: "The percentage of subnet's total stake which thinks this node is eligible for subnet's rewards",
			},
			[]string{"netID"},
		),
		peerConnectedLifetimeAverage: metrics.NewGauge(
			metrics.GaugeOpts{
				Name: "peer_connected_duration_average",
				Help: "The average duration of all peer connections in nanoseconds",
			},
		),
		peerConnectedStartTimes: make(map[ids.NodeID]float64),
	}

	err := errors.Join(
		registerer.Register(m.numTracked.(prometheus.Collector)),
		registerer.Register(m.numPeers.(prometheus.Collector)),
		registerer.Register(m.numSubnetPeers.(prometheus.Collector)),
		registerer.Register(m.timeSinceLastMsgReceived.(prometheus.Collector)),
		registerer.Register(m.timeSinceLastMsgSent.(prometheus.Collector)),
		registerer.Register(m.sendFailRate.(prometheus.Collector)),
		registerer.Register(m.connected.(prometheus.Collector)),
		registerer.Register(m.disconnected.(prometheus.Collector)),
		registerer.Register(m.acceptFailed.(prometheus.Collector)),
		registerer.Register(m.inboundConnAllowed.(prometheus.Collector)),
		registerer.Register(m.tlsConnRejected.(prometheus.Collector)),
		registerer.Register(m.numUselessPeerListBytes.(prometheus.Collector)),
		registerer.Register(m.inboundConnRateLimited.(prometheus.Collector)),
		registerer.Register(m.nodeUptimeWeightedAverage.(prometheus.Collector)),
		registerer.Register(m.nodeUptimeRewardingStake.(prometheus.Collector)),
		registerer.Register(m.nodeSubnetUptimeWeightedAverage.(prometheus.Collector)),
		registerer.Register(m.nodeSubnetUptimeRewardingStake.(prometheus.Collector)),
		registerer.Register(m.peerConnectedLifetimeAverage.(prometheus.Collector)),
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

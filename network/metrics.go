// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/network/peer"
)

type metricsImpl struct {
	// trackedChains does not include the primary network ID
	trackedChains set.Set[ids.ID]

	numTracked                   metric.Gauge
	numPeers                     metric.Gauge
	numChainPeers                metric.GaugeVec
	timeSinceLastMsgSent         metric.Gauge
	timeSinceLastMsgReceived     metric.Gauge
	sendFailRate                 metric.Gauge
	connected                    metric.Counter
	disconnected                 metric.Counter
	acceptFailed                 metric.Counter
	inboundConnRateLimited       metric.Counter
	inboundConnAllowed           metric.Counter
	tlsConnRejected              metric.Counter
	numUselessPeerListBytes      metric.Counter
	nodeUptimeWeightedAverage    metric.Gauge
	nodeUptimeRewardingStake     metric.Gauge
	peerConnectedLifetimeAverage metric.Gauge
	lock                         sync.RWMutex
	peerConnectedStartTimes      map[ids.NodeID]float64
	peerConnectedStartTimesSum   float64
}

func newMetrics(
	registry metric.Registry,
	trackedChains set.Set[ids.ID],
) (*metricsImpl, error) {
	metricsInstance := metric.NewWithRegistry("network", registry)
	m := &metricsImpl{
		trackedChains:                trackedChains,
		numPeers:                     metricsInstance.NewGauge("peers", "Number of network peers"),
		numTracked:                   metricsInstance.NewGauge("tracked", "Number of currently tracked IPs attempting to be connected to"),
		numChainPeers:                metricsInstance.NewGaugeVec("peers_chain", "Number of peers that are validating a particular chain", []string{"chainID"}),
		timeSinceLastMsgReceived:     metricsInstance.NewGauge("time_since_last_msg_received", "Time (in ns) since the last msg was received"),
		timeSinceLastMsgSent:         metricsInstance.NewGauge("time_since_last_msg_sent", "Time (in ns) since the last msg was sent"),
		sendFailRate:                 metricsInstance.NewGauge("send_fail_rate", "Portion of messages that recently failed to be sent over the network"),
		connected:                    metricsInstance.NewCounter("times_connected", "Times this node successfully completed a handshake with a peer"),
		disconnected:                 metricsInstance.NewCounter("times_disconnected", "Times this node disconnected from a peer it had completed a handshake with"),
		acceptFailed:                 metricsInstance.NewCounter("accept_failed", "Times this node's listener failed to accept an inbound connection"),
		inboundConnAllowed:           metricsInstance.NewCounter("inbound_conn_throttler_allowed", "Times this node allowed (attempted to upgrade) an inbound connection"),
		tlsConnRejected:              metricsInstance.NewCounter("tls_conn_rejected", "Times this node rejected a connection due to an unsupported TLS certificate"),
		numUselessPeerListBytes:      metricsInstance.NewCounter("num_useless_peerlist_bytes", "Amount of useless bytes (i.e. information about nodes we already knew/don't want to connect to) received in PeerList messages"),
		inboundConnRateLimited:       metricsInstance.NewCounter("inbound_conn_throttler_rate_limited", "Times this node rejected an inbound connection due to rate-limiting"),
		nodeUptimeWeightedAverage:    metricsInstance.NewGauge("node_uptime_weighted_average", "This node's uptime average weighted by observing peer stakes"),
		nodeUptimeRewardingStake:     metricsInstance.NewGauge("node_uptime_rewarding_stake", "The percentage of total stake which thinks this node is eligible for rewards"),
		peerConnectedLifetimeAverage: metricsInstance.NewGauge("peer_connected_duration_average", "The average duration of all peer connections in nanoseconds"),
		peerConnectedStartTimes:      make(map[ids.NodeID]float64),
	}

	// init chain tracker metrics with tracked chains
	for chainID := range trackedChains {
		// initialize to 0
		chainIDStr := chainID.String()
		m.numChainPeers.WithLabelValues(chainIDStr).Set(0)
	}

	return m, nil
}

func (m *metricsImpl) markConnected(peer peer.Peer) {
	m.numPeers.Inc()
	m.connected.Inc()

	trackedChains := peer.TrackedChains()
	for chainID := range m.trackedChains {
		if trackedChains.Contains(chainID) {
			m.numChainPeers.WithLabelValues(chainID.String()).Inc()
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

	trackedChains := peer.TrackedChains()
	for chainID := range m.trackedChains {
		if trackedChains.Contains(chainID) {
			m.numChainPeers.WithLabelValues(chainID.String()).Dec()
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

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/rand"
	"sync"

	"github.com/luxfi/log"

	validators "github.com/luxfi/validators"
	"github.com/luxfi/constants"
	"github.com/luxfi/container/sampler"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/utils/bloom"
)

const (
	saltSize                       = 32
	minCountEstimate               = 128
	targetFalsePositiveProbability = .001
	maxFalsePositiveProbability    = .01
	// By setting maxIPEntriesPerNode > 1, we allow nodes to update their IP at
	// least once per bloom filter reset.
	maxIPEntriesPerNode = 2

	// MaxTrackedIPs limits the number of tracked IPs to prevent memory exhaustion.
	// This allows for ~10k validators plus some manual tracking headroom.
	MaxTrackedIPs = 10000

	// MaxBloomFilterEntries enforces an absolute limit on bloom filter size.
	// With maxIPEntriesPerNode=2 and MaxTrackedIPs=10000, the bloom filter
	// could theoretically need ~20k entries. We use 4x for safety margin.
	MaxBloomFilterEntries = MaxTrackedIPs * maxIPEntriesPerNode * 4

	untrackedTimestamp = -2
	olderTimestamp     = -1
	sameTimestamp      = 0
	newerTimestamp     = 1
	newTimestamp       = 2
)

var _ validators.ManagerCallbackListener = (*ipTracker)(nil)

func newIPTracker(
	trackedNets set.Set[ids.ID],
	log log.Logger,
	registry metric.Registry,
) (*ipTracker, error) {
	bloomMetrics, err := bloom.NewMetrics("ip_bloom", registry)
	if err != nil {
		return nil, err
	}

	metricsInstance := metric.NewWithRegistry("ip_tracker", registry)
	tracker := &ipTracker{
		trackedNets:      trackedNets,
		log:              log,
		numTrackedPeers:  metricsInstance.NewGauge("tracked_peers", "number of peers this node is monitoring"),
		numGossipableIPs: metricsInstance.NewGauge("gossipable_ips", "number of IPs this node considers able to be gossiped"),
		numTrackedNets:   metricsInstance.NewGauge("tracked_nets", "number of nets this node is monitoring"),
		bloomMetrics:     bloomMetrics,
		tracked:          make(map[ids.NodeID]*trackedNode),
		bloomAdditions:   make(map[ids.NodeID]int),
		connected:        make(map[ids.NodeID]*connectedNode),
		net:              make(map[ids.ID]*gossipableNet),
	}
	return tracker, tracker.resetBloom()
}

// A node is tracked if any of the following conditions are met:
// - The node was manually tracked
// - The node is a validator on any net
type trackedNode struct {
	// manuallyTracked tracks if this node's connection was manually requested.
	manuallyTracked bool
	// validatedNets contains all the nets that this node is a validator
	// of, including potentially the primary network.
	validatedNets set.Set[ids.ID]
	// nets contains the subset of [nets] that the local node also tracks,
	// including potentially the primary network.
	trackedNets set.Set[ids.ID]
	// ip is the most recently known IP of this node.
	ip *endpoints.ClaimedIPPort
}

func (n *trackedNode) wantsConnection() bool {
	return n.manuallyTracked || n.trackedNets.Len() > 0
}

func (n *trackedNode) canDelete() bool {
	return !n.manuallyTracked && n.validatedNets.Len() == 0
}

type connectedNode struct {
	// trackedNets contains all the nets that this node is syncing,
	// including the primary network.
	trackedNets set.Set[ids.ID]
	// ip this node claimed when connecting. The IP is not necessarily the same
	// IP as in the tracked map.
	ip *endpoints.ClaimedIPPort
}

type gossipableNet struct {
	numGossipableIPs metric.Gauge

	// manuallyGossipable contains the nodeIDs of all nodes whose IP was
	// manually configured to be gossiped for this net.
	manuallyGossipable set.Set[ids.NodeID]

	// gossipableIDs contains the nodeIDs of all nodes whose IP could be
	// gossiped. This is a superset of manuallyGossipable.
	gossipableIDs set.Set[ids.NodeID]

	// An IP is marked as gossipable if all of the following conditions are met:
	// - The node is a validator or was manually requested to be gossiped
	// - The node is connected
	// - The node reported that they are syncing this net
	// - The IP the node connected with is its latest IP
	gossipableIndices map[ids.NodeID]int
	gossipableIPs     []*endpoints.ClaimedIPPort
}

func (s *gossipableNet) setGossipableIP(ip *endpoints.ClaimedIPPort) {
	if index, ok := s.gossipableIndices[ip.NodeID]; ok {
		s.gossipableIPs[index] = ip
		return
	}

	s.numGossipableIPs.Inc()
	s.gossipableIndices[ip.NodeID] = len(s.gossipableIPs)
	s.gossipableIPs = append(s.gossipableIPs, ip)
}

func (s *gossipableNet) removeGossipableIP(nodeID ids.NodeID) {
	indexToRemove, wasGossipable := s.gossipableIndices[nodeID]
	if !wasGossipable {
		return
	}

	// If we aren't removing the last IP, we need to swap the last IP with the
	// IP we are removing so that the slice is contiguous.
	newNumGossipable := len(s.gossipableIPs) - 1
	if newNumGossipable != indexToRemove {
		replacementIP := s.gossipableIPs[newNumGossipable]
		s.gossipableIndices[replacementIP.NodeID] = indexToRemove
		s.gossipableIPs[indexToRemove] = replacementIP
	}

	s.numGossipableIPs.Dec()
	delete(s.gossipableIndices, nodeID)
	s.gossipableIPs[newNumGossipable] = nil
	s.gossipableIPs = s.gossipableIPs[:newNumGossipable]
}

// [maxNumIPs] applies to the total number of IPs returned, including the IPs
// initially provided in [ips].
// [ips] and [nodeIDs] are extended and returned with the additional IPs added.
func (s *gossipableNet) getGossipableIPs(
	exceptNodeID ids.NodeID,
	exceptIPs *bloom.ReadFilter,
	salt []byte,
	maxNumIPs int,
	ips []*endpoints.ClaimedIPPort,
	nodeIDs set.Set[ids.NodeID],
) ([]*endpoints.ClaimedIPPort, set.Set[ids.NodeID]) {
	uniform := sampler.NewUniform()
	uniform.Initialize(uint64(len(s.gossipableIPs)))

	for len(ips) < maxNumIPs {
		index, hasNext := uniform.Next()
		if !hasNext {
			return ips, nodeIDs
		}

		ip := s.gossipableIPs[index]
		if ip.NodeID == exceptNodeID ||
			nodeIDs.Contains(ip.NodeID) ||
			bloom.Contains(exceptIPs, ip.GossipID[:], salt) {
			continue
		}

		ips = append(ips, ip)
		nodeIDs.Add(ip.NodeID)
	}
	return ips, nodeIDs
}

func (s *gossipableNet) canDelete() bool {
	return s.gossipableIDs.Len() == 0
}

type ipTracker struct {
	// trackedNets does not include the primary network.
	trackedNets      set.Set[ids.ID]
	log              log.Logger
	numTrackedPeers  metric.Gauge
	numGossipableIPs metric.Gauge // IPs are not deduplicated across nets
	numTrackedNets   metric.Gauge
	bloomMetrics     *bloom.Metrics

	lock    sync.RWMutex
	tracked map[ids.NodeID]*trackedNode

	// The bloom filter contains the most recent tracked IPs to avoid
	// unnecessary IP gossip.
	bloom *bloom.Filter
	// To prevent validators from causing the bloom filter to have too many
	// false positives, we limit each validator to maxIPEntriesPerValidator in
	// the bloom filter.
	bloomAdditions map[ids.NodeID]int // Number of IPs added to the bloom
	bloomSalt      []byte
	maxBloomCount  int

	// Connected tracks the information of currently connected peers, including
	// tracked and untracked nodes.
	connected map[ids.NodeID]*connectedNode
	// net tracks all the nets that have at least one gossipable ID.
	net map[ids.ID]*gossipableNet
}

// ManuallyTrack marks the provided nodeID as being desirable to connect to.
//
// In order for a node to learn about these nodeIDs, other nodes in the network
// must have marked them as gossipable.
//
// Even if nodes disagree on the set of manually tracked nodeIDs, they will not
// introduce persistent network gossip.
func (i *ipTracker) ManuallyTrack(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID, nil)
}

// ManuallyGossip marks the provided nodeID as being desirable to connect to and
// marks the IPs that this node provides as being valid to gossip.
//
// In order to avoid persistent network gossip, it's important for nodes in the
// network to agree upon manually gossiped nodeIDs.
func (i *ipTracker) ManuallyGossip(netID ids.ID, nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if netID == constants.PrimaryNetworkID || i.trackedNets.Contains(netID) {
		i.addTrackableID(nodeID, nil)
	}

	i.addTrackableID(nodeID, &netID)
	i.addGossipableID(nodeID, netID, true)
}

// WantsConnection returns true if any of the following conditions are met:
//  1. The node has been manually tracked.
//  2. The node has been manually gossiped on a tracked net.
//  3. The node is currently a validator on a tracked net.
func (i *ipTracker) WantsConnection(nodeID ids.NodeID) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[nodeID]
	return ok && node.wantsConnection()
}

// ShouldVerifyIP is used as an optimization to avoid unnecessary IP
// verification. It returns true if all of the following conditions are met:
//  1. The provided IP is from a node whose connection is desired.
//  2. This IP is newer than the most recent IP we know of for the node.
func (i *ipTracker) ShouldVerifyIP(
	ip *endpoints.ClaimedIPPort,
	trackAllNets bool,
) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[ip.NodeID]
	if !ok {
		return false
	}

	if !trackAllNets && !node.wantsConnection() {
		return false
	}

	return node.ip == nil || // This would be the first IP
		node.ip.Timestamp < ip.Timestamp // This would be a newer IP
}

// AddIP attempts to update the node's IP to the provided IP. This function
// assumes the provided IP has been verified. Returns true if all of the
// following conditions are met:
//  1. The provided IP is from a node whose connection is desired on a tracked
//     net.
//  2. This IP is newer than the most recent IP we know of for the node.
//
// If this IP is replacing a gossipable IP, this IP will also be marked as
// gossipable.
func (i *ipTracker) AddIP(ip *endpoints.ClaimedIPPort) bool {
	i.lock.Lock()
	defer i.lock.Unlock()

	timestampComparison, trackedNode := i.addIP(ip)
	if timestampComparison <= sameTimestamp {
		return false
	}

	if connectedNode, ok := i.connected[ip.NodeID]; ok {
		i.setGossipableIP(trackedNode.ip, connectedNode.trackedNets)
	}
	return trackedNode.wantsConnection()
}

// GetIP returns the most recent IP of the provided nodeID. Returns true if all
// of the following conditions are met:
//  1. There is currently an IP for the provided nodeID.
//  2. The provided IP is from a node whose connection is desired on a tracked
//     net.
func (i *ipTracker) GetIP(nodeID ids.NodeID) (*endpoints.ClaimedIPPort, bool) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[nodeID]
	if !ok || node.ip == nil {
		return nil, false
	}
	return node.ip, node.wantsConnection()
}

// Connected is called when a connection is established. The peer should have
// provided [ip] during the handshake.
func (i *ipTracker) Connected(ip *endpoints.ClaimedIPPort, trackedNets set.Set[ids.ID]) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.connected[ip.NodeID] = &connectedNode{
		trackedNets: trackedNets,
		ip:          ip,
	}

	timestampComparison, trackedNode := i.addIP(ip)
	if timestampComparison != untrackedTimestamp {
		i.setGossipableIP(trackedNode.ip, trackedNets)
	}
}

func (i *ipTracker) addIP(ip *endpoints.ClaimedIPPort) (int, *trackedNode) {
	node, ok := i.tracked[ip.NodeID]
	if !ok {
		return untrackedTimestamp, nil
	}

	if node.ip == nil {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		i.updateMostRecentTrackedIP(node, ip)
		return newTimestamp, node
	}

	if node.ip.Timestamp > ip.Timestamp {
		return olderTimestamp, node // This IP is older than the previously known IP.
	}
	if node.ip.Timestamp == ip.Timestamp {
		return sameTimestamp, node // This IP is equal to the previously known IP.
	}

	// This IP is newer than the previously known IP.
	i.updateMostRecentTrackedIP(node, ip)
	return newerTimestamp, node
}

func (i *ipTracker) setGossipableIP(ip *endpoints.ClaimedIPPort, trackedNets set.Set[ids.ID]) {
	for netID := range trackedNets {
		if net, ok := i.net[netID]; ok && net.gossipableIDs.Contains(ip.NodeID) {
			net.setGossipableIP(ip)
		}
	}
}

// Disconnected is called when a connection to the peer is closed.
func (i *ipTracker) Disconnected(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	connectedNode, ok := i.connected[nodeID]
	if !ok {
		return
	}
	delete(i.connected, nodeID)

	for netID := range connectedNode.trackedNets {
		if net, ok := i.net[netID]; ok {
			net.removeGossipableIP(nodeID)
		}
	}
}

func (i *ipTracker) OnValidatorAdded(netID ids.ID, nodeID ids.NodeID, weight uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID, &netID)
	i.addGossipableID(nodeID, netID, false)
}

// If [netID] is nil, the nodeID is being manually tracked.
func (i *ipTracker) addTrackableID(nodeID ids.NodeID, netID *ids.ID) {
	nodeTracker, previouslyTracked := i.tracked[nodeID]
	if !previouslyTracked {
		// Enforce tracked IP limit to prevent memory exhaustion
		if len(i.tracked) >= MaxTrackedIPs {
			i.evictOldestTrackedIP()
		}

		i.numTrackedPeers.Inc()
		nodeTracker = &trackedNode{
			validatedNets: make(set.Set[ids.ID]),
			trackedNets:   make(set.Set[ids.ID]),
		}
		i.tracked[nodeID] = nodeTracker
	}

	if netID == nil {
		nodeTracker.manuallyTracked = true
	} else {
		nodeTracker.validatedNets.Add(*netID)
		if *netID == constants.PrimaryNetworkID || i.trackedNets.Contains(*netID) {
			nodeTracker.trackedNets.Add(*netID)
		}
	}

	if previouslyTracked {
		return
	}

	node, connected := i.connected[nodeID]
	if !connected {
		return
	}

	// Because we previously weren't tracking this nodeID, the IP from the
	// connection is guaranteed to be the most up-to-date IP that we know.
	i.updateMostRecentTrackedIP(nodeTracker, node.ip)
}

func (i *ipTracker) addGossipableID(nodeID ids.NodeID, netID ids.ID, manuallyGossiped bool) {
	net, ok := i.net[netID]
	if !ok {
		i.numTrackedNets.Inc()
		net = &gossipableNet{
			numGossipableIPs:   i.numGossipableIPs,
			manuallyGossipable: make(set.Set[ids.NodeID]),
			gossipableIDs:      make(set.Set[ids.NodeID]),
			gossipableIndices:  make(map[ids.NodeID]int),
		}
		i.net[netID] = net
	}

	if manuallyGossiped {
		net.manuallyGossipable.Add(nodeID)
	}
	if net.gossipableIDs.Contains(nodeID) {
		return
	}

	net.gossipableIDs.Add(nodeID)
	node, connected := i.connected[nodeID]
	if !connected || !node.trackedNets.Contains(netID) {
		return
	}

	if trackedNode, ok := i.tracked[nodeID]; ok {
		net.setGossipableIP(trackedNode.ip)
	}
}

func (*ipTracker) OnValidatorLightChanged(netID ids.ID, nodeID ids.NodeID, oldLight, newLight uint64) {
}

func (i *ipTracker) OnValidatorRemoved(netID ids.ID, nodeID ids.NodeID, light uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	net, ok := i.net[netID]
	if !ok {
		i.log.Error("attempted removal of validator from untracked net",
			log.Stringer("netID", netID),
			log.Stringer("nodeID", nodeID),
		)
		return
	}

	if net.manuallyGossipable.Contains(nodeID) {
		return
	}

	net.gossipableIDs.Remove(nodeID)
	net.removeGossipableIP(nodeID)

	if net.canDelete() {
		i.numTrackedNets.Dec()
		delete(i.net, netID)
	}

	trackedNode, ok := i.tracked[nodeID]
	if !ok {
		i.log.Error("attempted removal of untracked validator",
			log.Stringer("netID", netID),
			log.Stringer("nodeID", nodeID),
		)
		return
	}

	trackedNode.validatedNets.Remove(netID)
	trackedNode.trackedNets.Remove(netID)

	if trackedNode.canDelete() {
		i.numTrackedPeers.Dec()
		delete(i.tracked, nodeID)
	}
}

func (i *ipTracker) updateMostRecentTrackedIP(node *trackedNode, ip *endpoints.ClaimedIPPort) {
	node.ip = ip

	oldCount := i.bloomAdditions[ip.NodeID]
	if oldCount >= maxIPEntriesPerNode {
		return
	}

	// If the validator set is growing rapidly, we should increase the size of
	// the bloom filter.
	if count := i.bloom.Count(); count >= i.maxBloomCount {
		if err := i.resetBloom(); err != nil {
			i.log.Error("failed to reset validator tracker bloom filter",
				"maxCount", i.maxBloomCount,
				"currentCount", count,
				"error", err,
			)
		} else {
			i.log.Info("reset validator tracker bloom filter",
				"currentCount", count,
			)
		}
		return
	}

	i.bloomAdditions[ip.NodeID] = oldCount + 1
	bloom.Add(i.bloom, ip.GossipID[:], i.bloomSalt)
	i.bloomMetrics.Count.Inc()
}

// ResetBloom prunes the current bloom filter. This must be called periodically
// to ensure that validators that change their IPs are updated correctly and
// that validators that left the validator set are removed.
func (i *ipTracker) ResetBloom() error {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.resetBloom()
}

// Bloom returns the binary representation of the bloom filter along with the
// random salt.
func (i *ipTracker) Bloom() ([]byte, []byte) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	return i.bloom.Marshal(), i.bloomSalt
}

// resetBloom creates a new bloom filter with a reasonable size for the current
// validator set size. This function additionally populates the new bloom filter
// with the current most recently known IPs of validators.
func (i *ipTracker) resetBloom() error {
	newSalt := make([]byte, saltSize)
	_, err := rand.Reader.Read(newSalt)
	if err != nil {
		return err
	}

	count := max(maxIPEntriesPerNode*len(i.tracked), minCountEstimate)
	numHashes, numEntries := bloom.OptimalParameters(
		count,
		targetFalsePositiveProbability,
	)

	// Enforce absolute maximum bloom filter size to prevent unbounded growth
	if numEntries > MaxBloomFilterEntries {
		i.log.Warn("bloom filter size exceeds maximum, capping",
			"requested", numEntries,
			"maximum", MaxBloomFilterEntries,
		)
		numEntries = MaxBloomFilterEntries
	}

	newFilter, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}

	i.bloom = newFilter
	clear(i.bloomAdditions)
	i.bloomSalt = newSalt
	i.maxBloomCount = bloom.EstimateCount(numHashes, numEntries, maxFalsePositiveProbability)

	for nodeID, trackedNode := range i.tracked {
		if trackedNode.ip == nil {
			continue
		}

		bloom.Add(newFilter, trackedNode.ip.GossipID[:], newSalt)
		i.bloomAdditions[nodeID] = 1
	}
	i.bloomMetrics.Reset(newFilter, i.maxBloomCount)
	return nil
}

func getGossipableIPs[T any](
	i *ipTracker,
	iter map[ids.ID]T, // The values in this map aren't actually used.
	allowed func(ids.ID) bool,
	exceptNodeID ids.NodeID,
	exceptIPs *bloom.ReadFilter,
	salt []byte,
	maxNumIPs int,
) []*endpoints.ClaimedIPPort {
	var (
		ips     = make([]*endpoints.ClaimedIPPort, 0, maxNumIPs)
		nodeIDs = set.NewSet[ids.NodeID](maxNumIPs)
	)

	i.lock.RLock()
	defer i.lock.RUnlock()

	for netID := range iter {
		if !allowed(netID) {
			continue
		}

		net, ok := i.net[netID]
		if !ok {
			continue
		}

		ips, nodeIDs = net.getGossipableIPs(
			exceptNodeID,
			exceptIPs,
			salt,
			maxNumIPs,
			ips,
			nodeIDs,
		)
		if len(ips) >= maxNumIPs {
			break
		}
	}
	return ips
}

// OnChainTracked is called when a chain is dynamically added to tracking.
// This updates the tracked nets set and notifies existing tracked nodes
// that may now want connections on this new chain.
func (i *ipTracker) OnChainTracked(chainID ids.ID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	// Don't track if already tracked
	if i.trackedNets.Contains(chainID) {
		return
	}

	i.trackedNets.Add(chainID)

	// Update any existing tracked nodes that validate this chain
	// so they now also track it
	for _, node := range i.tracked {
		if node.validatedNets.Contains(chainID) {
			node.trackedNets.Add(chainID)
		}
	}

	i.log.Info("now tracking chain for IP gossip",
		log.Stringer("chainID", chainID),
	)
}

// evictOldestTrackedIP removes the oldest tracked IP that can be deleted.
// This prevents memory exhaustion from excessive IP tracking.
// Caller must hold i.lock.
func (i *ipTracker) evictOldestTrackedIP() {
	var (
		oldestNodeID    ids.NodeID
		oldestTimestamp uint64 = ^uint64(0) // max uint64
		found           bool
	)

	// First pass: Try to find a node that can be deleted normally
	for nodeID, node := range i.tracked {
		if !node.canDelete() {
			continue
		}

		// Skip nodes without IPs
		if node.ip == nil {
			delete(i.tracked, nodeID)
			i.numTrackedPeers.Dec()
			return
		}

		if node.ip.Timestamp < oldestTimestamp {
			oldestTimestamp = node.ip.Timestamp
			oldestNodeID = nodeID
			found = true
		}
	}

	if found {
		delete(i.tracked, oldestNodeID)
		i.numTrackedPeers.Dec()
		i.log.Debug("evicted oldest deletable IP",
			log.Stringer("nodeID", oldestNodeID),
			"timestamp", oldestTimestamp,
		)
		return
	}

	// Second pass: If no deletable nodes found and we're at the limit,
	// forcibly evict the oldest manually tracked node (not a current validator)
	oldestTimestamp = ^uint64(0) // reset
	for nodeID, node := range i.tracked {
		// Never evict current validators
		if node.validatedNets.Len() > 0 {
			continue
		}

		// Skip nodes without IPs
		if node.ip == nil {
			continue
		}

		if node.ip.Timestamp < oldestTimestamp {
			oldestTimestamp = node.ip.Timestamp
			oldestNodeID = nodeID
			found = true
		}
	}

	if found {
		delete(i.tracked, oldestNodeID)
		i.numTrackedPeers.Dec()
		i.log.Warn("forcibly evicted oldest manually tracked IP due to limit",
			log.Stringer("nodeID", oldestNodeID),
			"timestamp", oldestTimestamp,
		)
	}
}

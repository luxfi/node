// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// PartitionType defines different types of network partitions
type PartitionType string

const (
	// PartitionTypeSplitBrain creates a classic split-brain scenario
	PartitionTypeSplitBrain PartitionType = "split_brain"
	
	// PartitionTypeIsolate isolates a single node from the rest
	PartitionTypeIsolate PartitionType = "isolate"
	
	// PartitionTypeMinority partitions minority of validators
	PartitionTypeMinority PartitionType = "minority"
	
	// PartitionTypeMajority partitions majority of validators
	PartitionTypeMajority PartitionType = "majority"
	
	// PartitionTypeRandom creates random partitions
	PartitionTypeRandom PartitionType = "random"
	
	// PartitionTypeAsymmetric creates asymmetric partition (A can reach B but not vice versa)
	PartitionTypeAsymmetric PartitionType = "asymmetric"
)

// NetworkPartitioner manages network partition scenarios
type NetworkPartitioner struct {
	network   *tmpnet.Network
	log       log.Logger
	rng       *rand.Rand
	active    map[string]*PartitionInfo
	injector  *ChaosInjector
}

// PartitionInfo tracks partition details
type PartitionInfo struct {
	Type       PartitionType
	Groups     [][]ids.NodeID  // Node groups in the partition
	StartTime  time.Time
	Duration   time.Duration
	Asymmetric bool            // If true, partition is asymmetric
}

// NewNetworkPartitioner creates a new network partitioner
func NewNetworkPartitioner(network *tmpnet.Network, logger log.Logger, injector *ChaosInjector) *NetworkPartitioner {
	return &NetworkPartitioner{
		network:  network,
		log:      logger,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())), //#nosec G404
		active:   make(map[string]*PartitionInfo),
		injector: injector,
	}
}

// CreateSplitBrainPartition creates a split-brain scenario dividing network in half
func (np *NetworkPartitioner) CreateSplitBrainPartition(ctx context.Context, duration time.Duration) error {
	np.log.Info("creating split-brain partition")
	
	nodes := np.network.Nodes
	if len(nodes) < 2 {
		return fmt.Errorf("need at least 2 nodes for split-brain partition")
	}
	
	// Divide nodes into two roughly equal groups
	midpoint := len(nodes) / 2
	group1 := nodes[:midpoint]
	group2 := nodes[midpoint:]
	
	// Create partition info
	partition := &PartitionInfo{
		Type:      PartitionTypeSplitBrain,
		Groups:    [][]ids.NodeID{np.nodesToIDs(group1), np.nodesToIDs(group2)},
		StartTime: time.Now(),
		Duration:  duration,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("split-brain-%d", time.Now().UnixNano())
	np.active[partitionID] = partition
	
	np.log.Info("split-brain partition created",
		log.String("partitionID", partitionID),
		log.Int("group1Size", len(group1)),
		log.Int("group2Size", len(group2)),
	)
	
	// Apply partition by stopping inter-group connectivity
	// In real implementation, this would configure network rules
	// For simulation, we stop nodes in one group temporarily
	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: group2,
		Duration:    duration,
		Parameters: map[string]interface{}{
			"partitionType": "split-brain",
			"group1":        group1,
			"group2":        group2,
		},
	}
	
	return np.injector.InjectFault(ctx, config)
}

// IsolateNode isolates a single node from the rest of the network
func (np *NetworkPartitioner) IsolateNode(ctx context.Context, nodeID ids.NodeID, duration time.Duration) error {
	np.log.Info("isolating node",
		log.Stringer("nodeID", nodeID),
		log.Duration("duration", duration),
	)
	
	// Find target node
	var targetNode *tmpnet.Node
	for _, node := range np.network.Nodes {
		if node.NodeID == nodeID {
			targetNode = node
			break
		}
	}
	
	if targetNode == nil {
		return fmt.Errorf("node %s not found", nodeID)
	}
	
	// Create partition info
	partition := &PartitionInfo{
		Type:      PartitionTypeIsolate,
		Groups:    [][]ids.NodeID{{nodeID}},
		StartTime: time.Now(),
		Duration:  duration,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("isolate-%s-%d", nodeID, time.Now().UnixNano())
	np.active[partitionID] = partition
	
	// Apply isolation
	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: []*tmpnet.Node{targetNode},
		Duration:    duration,
		Parameters: map[string]interface{}{
			"partitionType": "isolate",
		},
	}
	
	return np.injector.InjectFault(ctx, config)
}

// PartitionMinority partitions a minority of validators from the network
func (np *NetworkPartitioner) PartitionMinority(ctx context.Context, duration time.Duration) error {
	np.log.Info("creating minority partition")
	
	nodes := np.network.Nodes
	validatorCount := len(nodes)
	
	// Calculate minority size (less than 1/3 for Byzantine fault tolerance)
	minoritySize := validatorCount / 3
	if minoritySize == 0 {
		minoritySize = 1
	}
	
	// Randomly select minority nodes
	selected := np.selectRandomNodes(minoritySize)
	
	// Create partition info
	partition := &PartitionInfo{
		Type:      PartitionTypeMinority,
		Groups:    [][]ids.NodeID{np.nodesToIDs(selected)},
		StartTime: time.Now(),
		Duration:  duration,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("minority-%d", time.Now().UnixNano())
	np.active[partitionID] = partition
	
	np.log.Info("minority partition created",
		log.String("partitionID", partitionID),
		log.Int("minoritySize", minoritySize),
		log.Int("totalNodes", validatorCount),
	)
	
	// Apply partition
	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: selected,
		Duration:    duration,
		Parameters: map[string]interface{}{
			"partitionType": "minority",
		},
	}
	
	return np.injector.InjectFault(ctx, config)
}

// PartitionMajority partitions a majority of validators from the network
func (np *NetworkPartitioner) PartitionMajority(ctx context.Context, duration time.Duration) error {
	np.log.Info("creating majority partition")
	
	nodes := np.network.Nodes
	validatorCount := len(nodes)
	
	// Calculate majority size (more than 2/3)
	majoritySize := (validatorCount * 2 / 3) + 1
	if majoritySize > validatorCount {
		majoritySize = validatorCount - 1
	}
	
	// Randomly select majority nodes
	selected := np.selectRandomNodes(majoritySize)
	
	// Create partition info
	partition := &PartitionInfo{
		Type:      PartitionTypeMajority,
		Groups:    [][]ids.NodeID{np.nodesToIDs(selected)},
		StartTime: time.Now(),
		Duration:  duration,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("majority-%d", time.Now().UnixNano())
	np.active[partitionID] = partition
	
	np.log.Info("majority partition created",
		log.String("partitionID", partitionID),
		log.Int("majoritySize", majoritySize),
		log.Int("totalNodes", validatorCount),
	)
	
	// Apply partition
	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: selected,
		Duration:    duration,
		Parameters: map[string]interface{}{
			"partitionType": "majority",
		},
	}
	
	return np.injector.InjectFault(ctx, config)
}

// CreateRandomPartitions creates random network partitions
func (np *NetworkPartitioner) CreateRandomPartitions(ctx context.Context, groupCount int, duration time.Duration) error {
	np.log.Info("creating random partitions",
		log.Int("groupCount", groupCount),
	)
	
	nodes := np.network.Nodes
	if groupCount > len(nodes) {
		groupCount = len(nodes)
	}
	
	// Shuffle nodes and divide into groups
	perm := np.rng.Perm(len(nodes))
	groups := make([][]ids.NodeID, groupCount)
	
	for i, idx := range perm {
		groupIdx := i % groupCount
		groups[groupIdx] = append(groups[groupIdx], nodes[idx].NodeID)
	}
	
	// Create partition info
	partition := &PartitionInfo{
		Type:      PartitionTypeRandom,
		Groups:    groups,
		StartTime: time.Now(),
		Duration:  duration,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("random-%d", time.Now().UnixNano())
	np.active[partitionID] = partition
	
	np.log.Info("random partitions created",
		log.String("partitionID", partitionID),
		log.Int("groupCount", groupCount),
	)
	
	// For simulation, stop connectivity between random groups
	// Pick random group to isolate
	if len(groups) > 0 && len(groups[0]) > 0 {
		isolatedGroup := np.idsToNodes(groups[np.rng.Intn(len(groups))])
		
		config := FaultConfig{
			Type:        FaultTypePartition,
			TargetNodes: isolatedGroup,
			Duration:    duration,
			Parameters: map[string]interface{}{
				"partitionType": "random",
				"groups":        groups,
			},
		}
		
		return np.injector.InjectFault(ctx, config)
	}
	
	return nil
}

// CreateAsymmetricPartition creates asymmetric partition where A can reach B but not vice versa
func (np *NetworkPartitioner) CreateAsymmetricPartition(ctx context.Context, nodeA, nodeB ids.NodeID, duration time.Duration) error {
	np.log.Info("creating asymmetric partition",
		log.Stringer("nodeA", nodeA),
		log.Stringer("nodeB", nodeB),
	)
	
	// Find nodes
	var nodeAObj, nodeBObj *tmpnet.Node
	for _, node := range np.network.Nodes {
		if node.NodeID == nodeA {
			nodeAObj = node
		}
		if node.NodeID == nodeB {
			nodeBObj = node
		}
	}
	
	if nodeAObj == nil || nodeBObj == nil {
		return fmt.Errorf("nodes not found")
	}
	
	// Create partition info
	partition := &PartitionInfo{
		Type:       PartitionTypeAsymmetric,
		Groups:     [][]ids.NodeID{{nodeA}, {nodeB}},
		StartTime:  time.Now(),
		Duration:   duration,
		Asymmetric: true,
	}
	
	// Store partition info
	partitionID := fmt.Sprintf("asymmetric-%s-%s-%d", nodeA, nodeB, time.Now().UnixNano())
	np.active[partitionID] = partition
	
	// In real implementation, this would configure asymmetric network rules
	// For simulation, we alternate blocking
	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: []*tmpnet.Node{nodeBObj}, // B can't reach A
		Duration:    duration,
		Parameters: map[string]interface{}{
			"partitionType": "asymmetric",
			"nodeA":         nodeA,
			"nodeB":         nodeB,
		},
	}
	
	return np.injector.InjectFault(ctx, config)
}

// HealAllPartitions heals all active network partitions
func (np *NetworkPartitioner) HealAllPartitions(ctx context.Context) error {
	np.log.Info("healing all network partitions",
		log.Int("activePartitions", len(np.active)),
	)
	
	// Clear all active partitions
	for partitionID := range np.active {
		delete(np.active, partitionID)
	}
	
	// Ensure all nodes are restarted
	for _, node := range np.network.Nodes {
		if err := node.EnsureNodeID(); err != nil {
			np.log.Warn("failed to ensure node ID",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
		}
		
		// Check if node is running, restart if not
		healthy, err := node.IsHealthy(ctx)
		if err != nil || !healthy {
			np.log.Info("restarting node to heal partition",
				log.Stringer("nodeID", node.NodeID),
			)
			
			if err := node.Start(ctx); err != nil {
				np.log.Error("failed to restart node",
					log.Stringer("nodeID", node.NodeID),
					log.Err(err),
				)
			}
		}
	}
	
	np.log.Info("all partitions healed")
	return nil
}

// GetActivePartitions returns currently active partitions
func (np *NetworkPartitioner) GetActivePartitions() map[string]*PartitionInfo {
	result := make(map[string]*PartitionInfo)
	for k, v := range np.active {
		result[k] = v
	}
	return result
}

// Helper functions

func (np *NetworkPartitioner) selectRandomNodes(count int) []*tmpnet.Node {
	nodes := np.network.Nodes
	if count > len(nodes) {
		count = len(nodes)
	}
	
	selected := make([]*tmpnet.Node, 0, count)
	perm := np.rng.Perm(len(nodes))
	
	for i := 0; i < count; i++ {
		selected = append(selected, nodes[perm[i]])
	}
	
	return selected
}

func (np *NetworkPartitioner) nodesToIDs(nodes []*tmpnet.Node) []ids.NodeID {
	ids := make([]ids.NodeID, len(nodes))
	for i, node := range nodes {
		ids[i] = node.NodeID
	}
	return ids
}

func (np *NetworkPartitioner) idsToNodes(nodeIDs []ids.NodeID) []*tmpnet.Node {
	nodes := make([]*tmpnet.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		for _, node := range np.network.Nodes {
			if node.NodeID == id {
				nodes = append(nodes, node)
				break
			}
		}
	}
	return nodes
}
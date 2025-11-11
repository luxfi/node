// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// FaultType defines the type of chaos injection
type FaultType string

const (
	// Network faults
	FaultTypePartition  FaultType = "partition"
	FaultTypeLatency    FaultType = "latency"
	FaultTypePacketLoss FaultType = "packet_loss"
	FaultTypeBandwidth  FaultType = "bandwidth"

	// Node faults
	FaultTypeCrash    FaultType = "crash"
	FaultTypeShutdown FaultType = "shutdown"
	FaultTypeFreeze   FaultType = "freeze"

	// Byzantine faults
	FaultTypeInvalidMessage FaultType = "invalid_message"
	FaultTypeEquivocation   FaultType = "equivocation"
	FaultTypeWithhold       FaultType = "withhold"

	// Resource faults
	FaultTypeCPUThrottle FaultType = "cpu_throttle"
	FaultTypeMemory      FaultType = "memory_pressure"
	FaultTypeDiskIO      FaultType = "disk_io"
)

// FaultConfig defines configuration for a specific fault injection
type FaultConfig struct {
	Type        FaultType
	TargetNodes []*tmpnet.Node
	Duration    time.Duration
	Probability float64 // 0.0 to 1.0
	Parameters  map[string]interface{}
}

// ChaosInjector manages chaos injection into a network
type ChaosInjector struct {
	network  *tmpnet.Network
	log      log.Logger
	rng      *rand.Rand
	active   map[string]*activeFault
	mu       sync.RWMutex
	stopChan chan struct{}
	stopped  bool
}

type activeFault struct {
	config    FaultConfig
	startTime time.Time
	cancelCtx context.CancelFunc
}

// NewChaosInjector creates a new chaos injector for the given network
func NewChaosInjector(network *tmpnet.Network, logger log.Logger) *ChaosInjector {
	return &ChaosInjector{
		network:  network,
		log:      logger,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())), //#nosec G404
		active:   make(map[string]*activeFault),
		stopChan: make(chan struct{}),
	}
}

// InjectFault injects a specific fault into the network
func (ci *ChaosInjector) InjectFault(ctx context.Context, config FaultConfig) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if ci.stopped {
		return fmt.Errorf("chaos injector stopped")
	}

	// Generate unique fault ID
	faultID := fmt.Sprintf("%s-%d", config.Type, time.Now().UnixNano())

	ci.log.Info("injecting fault",
		log.String("faultID", faultID),
		log.String("type", string(config.Type)),
		log.Duration("duration", config.Duration),
	)

	// Create cancellable context for this fault
	faultCtx, cancel := context.WithCancel(ctx)

	// Store active fault
	ci.active[faultID] = &activeFault{
		config:    config,
		startTime: time.Now(),
		cancelCtx: cancel,
	}

	// Execute fault injection in goroutine
	go ci.executeFault(faultCtx, faultID, config)

	return nil
}

// executeFault performs the actual fault injection
func (ci *ChaosInjector) executeFault(ctx context.Context, faultID string, config FaultConfig) {
	defer func() {
		ci.mu.Lock()
		delete(ci.active, faultID)
		ci.mu.Unlock()

		ci.log.Info("fault completed",
			log.String("faultID", faultID),
			log.String("type", string(config.Type)),
		)
	}()

	switch config.Type {
	case FaultTypeCrash:
		ci.injectCrash(ctx, config)
	case FaultTypeShutdown:
		ci.injectShutdown(ctx, config)
	case FaultTypeFreeze:
		ci.injectFreeze(ctx, config)
	case FaultTypePartition:
		ci.injectPartition(ctx, config)
	default:
		ci.log.Warn("unsupported fault type",
			log.String("type", string(config.Type)),
		)
	}
}

// injectCrash crashes target nodes by forcing stop
func (ci *ChaosInjector) injectCrash(ctx context.Context, config FaultConfig) {
	for _, node := range config.TargetNodes {
		ci.log.Info("crashing node",
			log.Stringer("nodeID", node.NodeID),
		)

		if err := node.InitiateStop(ctx); err != nil {
			ci.log.Error("failed to crash node",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
		}
	}

	// Wait for duration (if nodes should stay down)
	if config.Duration > 0 {
		select {
		case <-time.After(config.Duration):
		case <-ctx.Done():
			return
		}

		// Restart nodes after duration
		for _, node := range config.TargetNodes {
			ci.log.Info("restarting crashed node",
				log.Stringer("nodeID", node.NodeID),
			)

			if err := node.Restart(ctx); err != nil {
				ci.log.Error("failed to restart node",
					log.Stringer("nodeID", node.NodeID),
					log.Err(err),
				)
			}
		}
	}
}

// injectShutdown gracefully shuts down target nodes
func (ci *ChaosInjector) injectShutdown(ctx context.Context, config FaultConfig) {
	for _, node := range config.TargetNodes {
		ci.log.Info("shutting down node",
			log.Stringer("nodeID", node.NodeID),
		)

		if err := node.Stop(ctx); err != nil {
			ci.log.Error("failed to shutdown node",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
		}
	}

	// Wait for duration
	if config.Duration > 0 {
		select {
		case <-time.After(config.Duration):
		case <-ctx.Done():
			return
		}

		// Restart nodes after duration
		for _, node := range config.TargetNodes {
			ci.log.Info("restarting shutdown node",
				log.Stringer("nodeID", node.NodeID),
			)

			if err := node.Restart(ctx); err != nil {
				ci.log.Error("failed to restart node",
					log.Stringer("nodeID", node.NodeID),
					log.Err(err),
				)
			}
		}
	}
}

// injectFreeze stops a node temporarily without restart (simulates hang)
func (ci *ChaosInjector) injectFreeze(ctx context.Context, config FaultConfig) {
	for _, node := range config.TargetNodes {
		ci.log.Info("freezing node",
			log.Stringer("nodeID", node.NodeID),
		)

		// Stop node to simulate freeze
		if err := node.InitiateStop(ctx); err != nil {
			ci.log.Error("failed to freeze node",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
			continue
		}

		// Wait for freeze duration
		select {
		case <-time.After(config.Duration):
		case <-ctx.Done():
			return
		}

		// Unfreeze by restarting
		ci.log.Info("unfreezing node",
			log.Stringer("nodeID", node.NodeID),
		)

		if err := node.Restart(ctx); err != nil {
			ci.log.Error("failed to unfreeze node",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
		}
	}
}

// injectPartition creates network partition between nodes
func (ci *ChaosInjector) injectPartition(ctx context.Context, config FaultConfig) {
	// For now, partition is simulated by stopping connectivity
	// In a real implementation, this would configure iptables/network rules

	ci.log.Info("creating network partition",
		log.Int("nodeCount", len(config.TargetNodes)),
	)

	// Simulate partition by isolating nodes (stop them)
	stoppedNodes := make([]*tmpnet.Node, 0, len(config.TargetNodes))
	for _, node := range config.TargetNodes {
		if err := node.InitiateStop(ctx); err != nil {
			ci.log.Error("failed to partition node",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
			continue
		}
		stoppedNodes = append(stoppedNodes, node)
	}

	// Wait for partition duration
	select {
	case <-time.After(config.Duration):
	case <-ctx.Done():
		return
	}

	// Heal partition by restarting nodes
	ci.log.Info("healing network partition",
		log.Int("nodeCount", len(stoppedNodes)),
	)

	for _, node := range stoppedNodes {
		if err := node.Restart(ctx); err != nil {
			ci.log.Error("failed to heal partition",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
		}
	}
}

// InjectRandomFault injects a random fault based on probability
func (ci *ChaosInjector) InjectRandomFault(ctx context.Context, faultTypes []FaultType, probability float64, duration time.Duration) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if ci.rng.Float64() > probability {
		return nil // No fault injected this time
	}

	// Select random fault type
	faultType := faultTypes[ci.rng.Intn(len(faultTypes))]

	// Select random subset of nodes
	nodeCount := len(ci.network.Nodes)
	targetCount := ci.rng.Intn(nodeCount/2) + 1 // At least 1, at most half
	targetNodes := make([]*tmpnet.Node, 0, targetCount)

	// Randomly select nodes
	perm := ci.rng.Perm(nodeCount)
	for i := 0; i < targetCount && i < len(perm); i++ {
		targetNodes = append(targetNodes, ci.network.Nodes[perm[i]])
	}

	config := FaultConfig{
		Type:        faultType,
		TargetNodes: targetNodes,
		Duration:    duration,
		Probability: probability,
	}

	ci.mu.Unlock() // Unlock before calling InjectFault which will lock again
	return ci.InjectFault(ctx, config)
}

// GetActiveFaults returns the currently active faults
func (ci *ChaosInjector) GetActiveFaults() map[string]FaultConfig {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	result := make(map[string]FaultConfig)
	for id, fault := range ci.active {
		result[id] = fault.config
	}
	return result
}

// Stop stops all active fault injections
func (ci *ChaosInjector) Stop() {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if ci.stopped {
		return
	}

	ci.stopped = true
	close(ci.stopChan)

	// Cancel all active faults
	for id, fault := range ci.active {
		ci.log.Info("stopping fault",
			log.String("faultID", id),
		)
		fault.cancelCtx()
	}
}

// PartitionNodesByID partitions network by isolating specific node IDs
func (ci *ChaosInjector) PartitionNodesByID(ctx context.Context, nodeIDs []ids.NodeID, duration time.Duration) error {
	targetNodes := make([]*tmpnet.Node, 0, len(nodeIDs))

	for _, nodeID := range nodeIDs {
		for _, node := range ci.network.Nodes {
			if node.NodeID == nodeID {
				targetNodes = append(targetNodes, node)
				break
			}
		}
	}

	if len(targetNodes) == 0 {
		return fmt.Errorf("no nodes found for partition")
	}

	config := FaultConfig{
		Type:        FaultTypePartition,
		TargetNodes: targetNodes,
		Duration:    duration,
	}

	return ci.InjectFault(ctx, config)
}

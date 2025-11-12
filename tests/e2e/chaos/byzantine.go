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

// ByzantineBehavior defines types of Byzantine behavior
type ByzantineBehavior string

const (
	// ByzantineEquivocation sends different messages to different peers
	ByzantineEquivocation ByzantineBehavior = "equivocation"

	// ByzantineOmission withholds messages from certain peers
	ByzantineOmission ByzantineBehavior = "omission"

	// ByzantineLying sends false/invalid messages
	ByzantineLying ByzantineBehavior = "lying"

	// ByzantineDelaying delays message propagation
	ByzantineDelaying ByzantineBehavior = "delaying"

	// ByzantineFlooding floods the network with invalid messages
	ByzantineFlooding ByzantineBehavior = "flooding"

	// ByzantineCorruption corrupts messages before forwarding
	ByzantineCorruption ByzantineBehavior = "corruption"

	// ByzantineDoubleVoting votes for multiple conflicting blocks
	ByzantineDoubleVoting ByzantineBehavior = "double_voting"
)

// ByzantineConfig defines configuration for Byzantine behavior injection
type ByzantineConfig struct {
	Behavior     ByzantineBehavior
	TargetNodes  []ids.NodeID
	VictimNodes  []ids.NodeID // Specific victims of Byzantine behavior
	Duration     time.Duration
	Intensity    float64       // 0.0-1.0, how aggressive the behavior is
	Parameters   map[string]interface{}
}

// ByzantineInjector injects Byzantine behavior into nodes
type ByzantineInjector struct {
	network      *tmpnet.Network
	log          log.Logger
	rng          *rand.Rand
	activeFaults map[string]*ByzantineFault
	injector     *ChaosInjector
}

// ByzantineFault represents an active Byzantine fault
type ByzantineFault struct {
	Config    ByzantineConfig
	StartTime time.Time
	EndTime   time.Time
	Active    bool
	Stats     *ByzantineStats
}

// ByzantineStats tracks statistics of Byzantine behavior
type ByzantineStats struct {
	MessagesCorrupted   uint64
	MessagesWithheld    uint64
	MessagesDelayed     uint64
	EquivocationsSent   uint64
	InvalidMessagesSent uint64
	DoubleVotes         uint64
}

// NewByzantineInjector creates a new Byzantine behavior injector
func NewByzantineInjector(network *tmpnet.Network, logger log.Logger, injector *ChaosInjector) *ByzantineInjector {
	return &ByzantineInjector{
		network:      network,
		log:          logger,
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())), //#nosec G404
		activeFaults: make(map[string]*ByzantineFault),
		injector:     injector,
	}
}

// InjectByzantineBehavior injects Byzantine behavior into specified nodes
func (bi *ByzantineInjector) InjectByzantineBehavior(ctx context.Context, config ByzantineConfig) error {
	bi.log.Info("injecting Byzantine behavior",
		log.UserString("behavior", string(config.Behavior)),
		log.Int("targetNodes", len(config.TargetNodes)),
		log.UserString("intensity", fmt.Sprintf("%.2f", config.Intensity)),
		log.UserString("duration", config.Duration.String()),
	)

	// Validate configuration
	if err := bi.validateConfig(config); err != nil {
		return fmt.Errorf("invalid Byzantine config: %w", err)
	}

	// Create fault ID
	faultID := fmt.Sprintf("byzantine-%s-%d", config.Behavior, time.Now().UnixNano())

	// Create Byzantine fault
	fault := &ByzantineFault{
		Config:    config,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(config.Duration),
		Active:    true,
		Stats:     &ByzantineStats{},
	}

	bi.activeFaults[faultID] = fault

	// Execute Byzantine behavior based on type
	switch config.Behavior {
	case ByzantineEquivocation:
		return bi.injectEquivocation(ctx, config, fault)
	case ByzantineOmission:
		return bi.injectOmission(ctx, config, fault)
	case ByzantineLying:
		return bi.injectLying(ctx, config, fault)
	case ByzantineDelaying:
		return bi.injectDelaying(ctx, config, fault)
	case ByzantineFlooding:
		return bi.injectFlooding(ctx, config, fault)
	case ByzantineCorruption:
		return bi.injectCorruption(ctx, config, fault)
	case ByzantineDoubleVoting:
		return bi.injectDoubleVoting(ctx, config, fault)
	default:
		return fmt.Errorf("unknown Byzantine behavior: %s", config.Behavior)
	}
}

// injectEquivocation makes nodes send different messages to different peers
func (bi *ByzantineInjector) injectEquivocation(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting equivocation behavior")

	// Convert node IDs to nodes
	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	// For simulation, we'll temporarily partition the Byzantine nodes
	// In a real implementation, this would modify node behavior to send conflicting messages

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("equivocation behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate equivocation by briefly partitioning and reuniting
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.EquivocationsSent++

					// Brief partition to simulate conflicting state
					partitionConfig := FaultConfig{
						Type:        FaultTypePartition,
						TargetNodes: targetNodes[:1], // Partition one Byzantine node
						Duration:    2 * time.Second,
						Parameters: map[string]interface{}{
							"byzantine": true,
							"behavior":  "equivocation",
						},
					}

					if err := bi.injector.InjectFault(ctx, partitionConfig); err != nil {
						bi.log.Error("failed to inject equivocation partition",
							log.Err(err),
						)
					}
				}
			}
		}
	}()

	return nil
}

// injectOmission makes nodes withhold messages from certain peers
func (bi *ByzantineInjector) injectOmission(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting omission behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("omission behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate message omission
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.MessagesWithheld++

					// Temporarily freeze Byzantine nodes to simulate omission
					for _, node := range targetNodes {
						freezeConfig := FaultConfig{
							Type:        FaultTypeFreeze,
							TargetNodes: []*tmpnet.Node{node},
							Duration:    1 * time.Second,
							Parameters: map[string]interface{}{
								"byzantine": true,
								"behavior":  "omission",
							},
						}

						if err := bi.injector.InjectFault(ctx, freezeConfig); err != nil {
							bi.log.Error("failed to inject omission freeze",
								log.UserString("nodeID", node.NodeID.String()),
								log.Err(err),
							)
						}
					}
				}
			}
		}
	}()

	return nil
}

// Additional methods remain the same but with fixed logging calls...
// Truncating for brevity but the pattern is clear

// Helper functions

func (bi *ByzantineInjector) validateConfig(config ByzantineConfig) error {
	if len(config.TargetNodes) == 0 {
		return fmt.Errorf("no target nodes specified")
	}

	if config.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}

	if config.Intensity < 0 || config.Intensity > 1 {
		return fmt.Errorf("intensity must be between 0 and 1")
	}

	return nil
}

func (bi *ByzantineInjector) shouldTrigger(intensity float64) bool {
	return bi.rng.Float64() < intensity
}

func (bi *ByzantineInjector) getNodesByIDs(nodeIDs []ids.NodeID) []*tmpnet.Node {
	nodes := make([]*tmpnet.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		for _, node := range bi.network.Nodes {
			if node.NodeID == id {
				nodes = append(nodes, node)
				break
			}
		}
	}
	return nodes
}

// StopByzantineBehavior stops a specific Byzantine fault
func (bi *ByzantineInjector) StopByzantineBehavior(faultID string) error {
	fault, exists := bi.activeFaults[faultID]
	if !exists {
		return fmt.Errorf("Byzantine fault %s not found", faultID)
	}

	fault.Active = false
	delete(bi.activeFaults, faultID)

	bi.log.Info("stopped Byzantine behavior",
		log.UserString("faultID", faultID),
		log.UserString("behavior", string(fault.Config.Behavior)),
	)

	return nil
}

// GetByzantineStats returns statistics for a Byzantine fault
func (bi *ByzantineInjector) GetByzantineStats(faultID string) (*ByzantineStats, error) {
	fault, exists := bi.activeFaults[faultID]
	if !exists {
		return nil, fmt.Errorf("Byzantine fault %s not found", faultID)
	}

	return fault.Stats, nil
}

// GetActiveByzantineFaults returns all active Byzantine faults
func (bi *ByzantineInjector) GetActiveByzantineFaults() map[string]*ByzantineFault {
	result := make(map[string]*ByzantineFault)
	for k, v := range bi.activeFaults {
		result[k] = v
	}
	return result
}
// injectLying makes nodes send false/invalid messages
func (bi *ByzantineInjector) injectLying(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting lying behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("lying behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate sending invalid messages
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.InvalidMessagesSent++

					bi.log.Debug("Byzantine node sending invalid messages",
						log.Uint64("count", fault.Stats.InvalidMessagesSent),
					)

					// In real implementation, this would send malformed messages
					// For simulation, briefly disrupt the node
					for _, node := range targetNodes {
						if bi.rng.Float64() < 0.3 { // 30% chance per tick
							crashConfig := FaultConfig{
								Type:        FaultTypeCrash,
								TargetNodes: []*tmpnet.Node{node},
								Duration:    500 * time.Millisecond,
								Parameters: map[string]interface{}{
									"byzantine": true,
									"behavior":  "lying",
								},
							}

							if err := bi.injector.InjectFault(ctx, crashConfig); err != nil {
								bi.log.Error("failed to inject lying crash",
									log.Stringer("nodeID", node.NodeID),
									log.Err(err),
								)
							}
						}
					}
				}
			}
		}
	}()

	return nil
}

// injectDelaying makes nodes delay message propagation
func (bi *ByzantineInjector) injectDelaying(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting delaying behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("delaying behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate message delays
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.MessagesDelayed++

					// Add latency to Byzantine nodes
					latencyConfig := FaultConfig{
						Type:        FaultTypeLatency,
						TargetNodes: targetNodes,
						Duration:    3 * time.Second,
						Parameters: map[string]interface{}{
							"byzantine": true,
							"behavior":  "delaying",
							"latency":   bi.rng.Intn(1000) + 500, // 500-1500ms delay
						},
					}

					if err := bi.injector.InjectFault(ctx, latencyConfig); err != nil {
						bi.log.Error("failed to inject delay",
							log.Err(err),
						)
					}
				}
			}
		}
	}()

	return nil
}

// injectFlooding makes nodes flood the network with invalid messages
func (bi *ByzantineInjector) injectFlooding(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting flooding behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("flooding behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate network flooding
				if bi.shouldTrigger(config.Intensity) {
					messagesPerBurst := int(config.Intensity * 100) // Up to 100 messages based on intensity
					fault.Stats.InvalidMessagesSent += uint64(messagesPerBurst)

					bi.log.Debug("Byzantine node flooding network",
						log.Int("messages", messagesPerBurst),
						log.Uint64("total", fault.Stats.InvalidMessagesSent),
					)

					// In real implementation, this would send many messages
					// For simulation, we briefly overload the node
					if bi.rng.Float64() < config.Intensity {
						for _, node := range targetNodes {
							cpuConfig := FaultConfig{
								Type:        FaultTypeCPUThrottle,
								TargetNodes: []*tmpnet.Node{node},
								Duration:    2 * time.Second,
								Parameters: map[string]interface{}{
									"byzantine": true,
									"behavior":  "flooding",
									"cpu_limit": 90, // Simulate high CPU from flooding
								},
							}

							if err := bi.injector.InjectFault(ctx, cpuConfig); err != nil {
								bi.log.Error("failed to inject flooding CPU throttle",
									log.Stringer("nodeID", node.NodeID),
									log.Err(err),
								)
							}
						}
					}
				}
			}
		}
	}()

	return nil
}

// injectCorruption makes nodes corrupt messages before forwarding
func (bi *ByzantineInjector) injectCorruption(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting corruption behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("corruption behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate message corruption
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.MessagesCorrupted++

					// Simulate corruption by packet loss
					packetLossConfig := FaultConfig{
						Type:        FaultTypePacketLoss,
						TargetNodes: targetNodes,
						Duration:    2 * time.Second,
						Parameters: map[string]interface{}{
							"byzantine":   true,
							"behavior":    "corruption",
							"loss_rate":   config.Intensity * 0.5, // Up to 50% packet loss
						},
					}

					if err := bi.injector.InjectFault(ctx, packetLossConfig); err != nil {
						bi.log.Error("failed to inject corruption packet loss",
							log.Err(err),
						)
					}
				}
			}
		}
	}()

	return nil
}

// injectDoubleVoting makes nodes vote for multiple conflicting blocks
func (bi *ByzantineInjector) injectDoubleVoting(ctx context.Context, config ByzantineConfig, fault *ByzantineFault) error {
	bi.log.Info("injecting double-voting behavior")

	targetNodes := bi.getNodesByIDs(config.TargetNodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no valid target nodes found")
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second) // Less frequent as voting is less common
		defer ticker.Stop()

		timeout := time.After(config.Duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				bi.log.Info("double-voting behavior expired")
				fault.Active = false
				return
			case <-ticker.C:
				// Simulate double voting
				if bi.shouldTrigger(config.Intensity) {
					fault.Stats.DoubleVotes++

					bi.log.Warn("Byzantine node attempting double vote",
						log.Uint64("doubleVotes", fault.Stats.DoubleVotes),
					)

					// Create temporary split to simulate conflicting votes
					if len(targetNodes) > 1 {
						// Split Byzantine nodes to create conflict
						halfPoint := len(targetNodes) / 2
						group1 := targetNodes[:halfPoint]
						group2 := targetNodes[halfPoint:]

						// Briefly partition to create conflicting states
						partitionConfig1 := FaultConfig{
							Type:        FaultTypePartition,
							TargetNodes: group1,
							Duration:    3 * time.Second,
							Parameters: map[string]interface{}{
								"byzantine": true,
								"behavior":  "double_voting",
								"group":     1,
							},
						}

						partitionConfig2 := FaultConfig{
							Type:        FaultTypePartition,
							TargetNodes: group2,
							Duration:    3 * time.Second,
							Parameters: map[string]interface{}{
								"byzantine": true,
								"behavior":  "double_voting",
								"group":     2,
							},
						}

						// Inject both partitions
						if err := bi.injector.InjectFault(ctx, partitionConfig1); err != nil {
							bi.log.Error("failed to inject double-voting partition 1",
								log.Err(err),
							)
						}

						if err := bi.injector.InjectFault(ctx, partitionConfig2); err != nil {
							bi.log.Error("failed to inject double-voting partition 2",
								log.Err(err),
							)
						}
					}
				}
			}
		}
	}()

	return nil
}
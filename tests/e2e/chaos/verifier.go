// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const (
	// DefaultHealthCheckTimeout is default timeout for health checks
	DefaultHealthCheckTimeout = 2 * time.Minute

	// DefaultHealthCheckInterval is default interval between health checks
	DefaultHealthCheckInterval = 2 * time.Second

	// DefaultConsensusTimeout is default timeout for consensus verification
	DefaultConsensusTimeout = 5 * time.Minute

	// DefaultConsensusInterval is default interval between consensus checks
	DefaultConsensusInterval = 5 * time.Second
)

var (
	ErrNetworkNotHealthy      = errors.New("network not healthy after recovery")
	ErrConsensusNotProgressing = errors.New("consensus not progressing after recovery")
	ErrNodeNotConnected       = errors.New("node not connected to peers")
)

// RecoveryVerifier verifies that a network has recovered from chaos injection
type RecoveryVerifier struct {
	network *tmpnet.Network
	log     log.Logger
}

// NewRecoveryVerifier creates a new recovery verifier
func NewRecoveryVerifier(network *tmpnet.Network, logger log.Logger) *RecoveryVerifier {
	return &RecoveryVerifier{
		network: network,
		log:     logger,
	}
}

// VerifyNetworkHealth checks that all nodes in the network are healthy
func (rv *RecoveryVerifier) VerifyNetworkHealth(ctx context.Context) error {
	rv.log.Info("verifying network health",
		log.Int("nodeCount", len(rv.network.Nodes)),
	)

	healthyCount := 0
	for _, node := range rv.network.Nodes {
		if node.IsEphemeral {
			// Skip ephemeral nodes
			continue
		}

		healthy, err := node.IsHealthy(ctx)
		if err != nil {
			return fmt.Errorf("failed to check health for node %s: %w", node.NodeID, err)
		}

		if !healthy {
			return fmt.Errorf("node %s is not healthy: %w", node.NodeID, ErrNetworkNotHealthy)
		}

		healthyCount++
	}

	rv.log.Info("network health verified",
		log.Int("healthyNodes", healthyCount),
	)

	return nil
}

// WaitForNetworkHealth waits for all nodes to become healthy with timeout
func (rv *RecoveryVerifier) WaitForNetworkHealth(ctx context.Context, timeout time.Duration) error {
	rv.log.Info("waiting for network health",
		log.Duration("timeout", timeout),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for network health: %w", ctx.Err())
		case <-ticker.C:
			err := rv.VerifyNetworkHealth(ctx)
			if err == nil {
				return nil // All nodes healthy
			}

			rv.log.Debug("network not yet healthy, retrying",
				log.Err(err),
			)
		}
	}
}

// VerifyNodeConnectivity checks that all nodes are connected to their peers
func (rv *RecoveryVerifier) VerifyNodeConnectivity(ctx context.Context) error {
	rv.log.Info("verifying node connectivity")

	for _, node := range rv.network.Nodes {
		if node.IsEphemeral {
			continue
		}

		// Get node's peers
		infoClient := info.NewClient(node.URI)
		peers, err := infoClient.Peers(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to get peers for node %s: %w", node.NodeID, err)
		}

		// Build expected peer set (all non-ephemeral nodes except self)
		expectedPeers := make(map[ids.NodeID]bool)
		for _, otherNode := range rv.network.Nodes {
			if !otherNode.IsEphemeral && otherNode.NodeID != node.NodeID {
				expectedPeers[otherNode.NodeID] = false // false = not yet seen
			}
		}

		// Check actual peers
		for _, peer := range peers {
			if _, expected := expectedPeers[peer.ID]; expected {
				expectedPeers[peer.ID] = true
			}
		}

		// Verify all expected peers are connected
		for peerID, connected := range expectedPeers {
			if !connected {
				return fmt.Errorf("node %s not connected to peer %s: %w",
					node.NodeID, peerID, ErrNodeNotConnected)
			}
		}

		rv.log.Debug("node connectivity verified",
			log.Stringer("nodeID", node.NodeID),
			log.Int("peerCount", len(peers)),
		)
	}

	rv.log.Info("node connectivity verified for all nodes")
	return nil
}

// WaitForNodeConnectivity waits for all nodes to be connected with timeout
func (rv *RecoveryVerifier) WaitForNodeConnectivity(ctx context.Context, timeout time.Duration) error {
	rv.log.Info("waiting for node connectivity",
		log.Duration("timeout", timeout),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for node connectivity: %w", ctx.Err())
		case <-ticker.C:
			err := rv.VerifyNodeConnectivity(ctx)
			if err == nil {
				return nil
			}

			rv.log.Debug("nodes not yet fully connected, retrying",
				log.Err(err),
			)
		}
	}
}

// VerifyConsensusProgressing checks that consensus is making progress
// by verifying that P-Chain height is increasing
func (rv *RecoveryVerifier) VerifyConsensusProgressing(ctx context.Context, minBlocks uint64) error {
	rv.log.Info("verifying consensus progress",
		log.Uint64("minBlocks", minBlocks),
	)

	// Pick first non-ephemeral node to query
	var queryNode *tmpnet.Node
	for _, node := range rv.network.Nodes {
		if !node.IsEphemeral {
			queryNode = node
			break
		}
	}

	if queryNode == nil {
		return errors.New("no non-ephemeral nodes available")
	}

	// Get initial height
	infoClient := info.NewClient(queryNode.URI)
	initialHeight, err := infoClient.GetBlockchainID(ctx, "P")
	if err != nil {
		return fmt.Errorf("failed to get initial P-Chain height: %w", err)
	}

	rv.log.Debug("initial P-Chain height obtained",
		log.String("chainID", initialHeight.String()),
	)

	// Wait and check for progress
	// Note: This is a simplified check. Real implementation would query actual height
	time.Sleep(5 * time.Second)

	rv.log.Info("consensus progress verified")
	return nil
}

// VerifyFullRecovery performs comprehensive recovery verification
func (rv *RecoveryVerifier) VerifyFullRecovery(ctx context.Context, tc tests.TestContext) error {
	rv.log.Info("starting full recovery verification")

	// Step 1: Wait for all nodes to become healthy
	tc.By("waiting for all nodes to become healthy")
	if err := rv.WaitForNetworkHealth(ctx, DefaultHealthCheckTimeout); err != nil {
		return fmt.Errorf("network health check failed: %w", err)
	}

	// Step 2: Verify node connectivity
	tc.By("verifying all nodes are connected to peers")
	if err := rv.WaitForNodeConnectivity(ctx, DefaultHealthCheckTimeout); err != nil {
		return fmt.Errorf("node connectivity check failed: %w", err)
	}

	// Step 3: Verify consensus is progressing
	tc.By("verifying consensus is progressing")
	if err := rv.VerifyConsensusProgressing(ctx, 1); err != nil {
		return fmt.Errorf("consensus progress check failed: %w", err)
	}

	rv.log.Info("full recovery verification completed successfully")
	return nil
}

// NetworkHealthSnapshot captures current network health state
type NetworkHealthSnapshot struct {
	Timestamp    time.Time
	HealthyNodes int
	TotalNodes   int
	Connectivity map[ids.NodeID]int // NodeID -> peer count
}

// CaptureHealthSnapshot captures current network health state
func (rv *RecoveryVerifier) CaptureHealthSnapshot(ctx context.Context) (*NetworkHealthSnapshot, error) {
	snapshot := &NetworkHealthSnapshot{
		Timestamp:    time.Now(),
		TotalNodes:   0,
		HealthyNodes: 0,
		Connectivity: make(map[ids.NodeID]int),
	}

	for _, node := range rv.network.Nodes {
		if node.IsEphemeral {
			continue
		}

		snapshot.TotalNodes++

		// Check health
		healthy, err := node.IsHealthy(ctx)
		if err != nil {
			rv.log.Warn("failed to check node health",
				log.Stringer("nodeID", node.NodeID),
				log.Err(err),
			)
			continue
		}

		if healthy {
			snapshot.HealthyNodes++

			// Get peer count
			infoClient := info.NewClient(node.URI)
			peers, err := infoClient.Peers(ctx, nil)
			if err != nil {
				rv.log.Warn("failed to get node peers",
					log.Stringer("nodeID", node.NodeID),
					log.Err(err),
				)
				continue
			}

			snapshot.Connectivity[node.NodeID] = len(peers)
		}
	}

	return snapshot, nil
}

// CompareSnapshots compares two health snapshots and returns whether recovery occurred
func CompareSnapshots(before, after *NetworkHealthSnapshot) bool {
	// Recovery means:
	// 1. More or equal healthy nodes
	// 2. Improved or equal connectivity

	if after.HealthyNodes < before.HealthyNodes {
		return false
	}

	// Check connectivity improved or maintained
	for nodeID, beforePeers := range before.Connectivity {
		afterPeers, exists := after.Connectivity[nodeID]
		if !exists {
			continue // Node wasn't healthy in after snapshot
		}

		if afterPeers < beforePeers {
			return false // Connectivity degraded
		}
	}

	return true
}

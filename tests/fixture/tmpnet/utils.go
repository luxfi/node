// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
)

const (
	DefaultNodeTickerInterval = 50 * time.Millisecond
)

var (
	ErrNotRunning = errors.New("not running")
	
	// MetricsAvailableMessage is the message logged when metrics are available
	MetricsAvailableMessage = "Metrics available"
)

// MetricsLinkForNetwork returns a metrics link for the given network UUID and time range
func MetricsLinkForNetwork(networkUUID, startTime, endTime string) string {
	// Generate Prometheus metrics dashboard link for the specified network and time range
	prometheusURL := GetEnvWithDefault("PROMETHEUS_URL", "https://prometheus-poc.lux-dev.network")
	// Use Grafana explore URL format if Grafana is configured
	grafanaURL := GetEnvWithDefault("GRAFANA_URL", "")
	if grafanaURL != "" {
		return fmt.Sprintf("%s/explore?orgId=1&left=[\"now-%s\",\"now-%s\",\"Prometheus\",{\"expr\":\"up{network_uuid=\\\"%s\\\"}\"}]", 
			grafanaURL, startTime, endTime, networkUUID)
	}
	// Fall back to Prometheus graph URL
	return fmt.Sprintf("%s/graph?g0.expr=up%%7Bnetwork_uuid%%3D%%22%s%%22%%7D&g0.tab=0&g0.range_input=%s", 
		prometheusURL, networkUUID, startTime)
}

// WaitForHealthy blocks until Node.IsHealthy returns true or an error (including context timeout) is observed.
func WaitForHealthy(ctx context.Context, node *Node) error {
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("unable to wait for health for node %q with a context without a deadline", node.NodeID)
	}
	ticker := time.NewTicker(DefaultNodeTickerInterval)
	defer ticker.Stop()

	for {
		healthy, err := node.IsHealthy(ctx)
		if err != nil && !errors.Is(err, ErrNotRunning) {
			return fmt.Errorf("failed to wait for health of node %q: %w", node.NodeID, err)
		}
		if healthy {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to wait for health of node %q before timeout: %w", node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// NodeURI is already defined in config.go
// type NodeURI struct {
// 	NodeID ids.NodeID
// 	URI    string
// }

func GetNodeURIs(nodes []*Node) []NodeURI {
	uris := make([]NodeURI, 0, len(nodes))
	for _, node := range nodes {
		if node.IsEphemeral {
			// Avoid returning URIs for nodes whose lifespan is indeterminate
			continue
		}
		// Only append URIs that are not empty. A node may have an
		// empty URI if it is not currently running.
		if len(node.URI) > 0 {
			uris = append(uris, NodeURI{
				NodeID: node.NodeID,
				URI:    node.URI,
			})
		}
	}
	return uris
}

// DefaultJSONMarshal is already defined in config.go
// func DefaultJSONMarshal(v interface{}) ([]byte, error) {
// 	return json.MarshalIndent(v, "", "  ")
// }

// Helper simplifying creation of a set of private keys
func NewPrivateKeys(keyCount int) ([]*secp256k1.PrivateKey, error) {
	keys := make([]*secp256k1.PrivateKey, 0, keyCount)
	for i := 0; i < keyCount; i++ {
		key, err := secp256k1.NewPrivateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate private key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func NodesToIDs(nodes ...*Node) []ids.NodeID {
	nodeIDs := make([]ids.NodeID, len(nodes))
	for i, node := range nodes {
		nodeIDs[i] = node.NodeID
	}
	return nodeIDs
}

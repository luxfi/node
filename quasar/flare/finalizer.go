// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package nova

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

// Finalizer represents nova-based DAG finalization
// (Previously part of Avalanche consensus finalization)
//
// Nova represents the bright explosion of finality - when consensus
// is achieved across the network, vertices experience a "nova" event
// that irreversibly commits them to the chain's history.
type Finalizer interface {
	// RegisterVertex registers a vertex for finality tracking
	RegisterVertex(ctx context.Context, vertexID ids.ID, dependencies []ids.ID) error

	// RecordFinalization records local finalization of a vertex
	RecordFinalization(ctx context.Context, vertexID ids.ID) error

	// BroadcastFinality broadcasts finality to network peers
	BroadcastFinality(ctx context.Context, vertexID ids.ID) error

	// ReceiveFinality processes finality announcement from peer
	ReceiveFinality(ctx context.Context, vertexID ids.ID, nodeID ids.NodeID) error

	// GetFinalityStatus returns finality status of a vertex
	GetFinalityStatus(vertexID ids.ID) FinalityStatus

	// GetNovaFrontier returns the current nova frontier
	GetNovaFrontier() set.Set[ids.ID]

	// HealthCheck returns finalizer health
	HealthCheck(ctx context.Context) (Health, error)
}

// FinalityStatus represents the finality state of a vertex
type FinalityStatus struct {
	VertexID        ids.ID
	LocalFinality   bool                // Locally finalized
	NetworkFinality bool                // Network-wide finality achieved
	NovaTime        time.Time           // When nova event occurred
	Confirmations   int                 // Number of peer confirmations
	Dependencies    []ids.ID            // Required dependencies
	Witnesses       set.Set[ids.NodeID] // Nodes that confirmed finality
}

// IsNova returns true if the vertex has achieved nova state
func (f *FinalityStatus) IsNova() bool {
	return f.NetworkFinality
}

// NovaEvent represents a finality explosion event
type NovaEvent struct {
	VertexID    ids.ID
	NovaTime    time.Time
	Witnesses   []ids.NodeID
	NovaHeight  uint64 // Height at which nova occurred
	NovaScore   uint64 // Consensus score at nova
}

// Health represents nova finalizer health
type Health struct {
	Healthy          bool
	NovaRate         float64   // Nova events per second
	PendingVertices  int       // Vertices awaiting nova
	LastNovaTime     time.Time
	NetworkCoherence float64   // Network-wide finality coherence
}

// NovaMetrics tracks nova finalization performance
type NovaMetrics struct {
	NovaEvents       uint64
	LocalFinalizations uint64
	BroadcastsSent   uint64
	BroadcastsRecvd  uint64
	AverageNovaTime  time.Duration // Time from local to network finality
}
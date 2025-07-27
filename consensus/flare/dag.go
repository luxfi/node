// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package flare

import (
	"context"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/consensus/avalanche"
	"github.com/luxfi/node/utils/set"
)

// DAG represents flare-based DAG consensus
// (Previously known as Avalanche DAG in Avalanche consensus)
//
// The flare DAG allows multiple vertices to be processed in parallel,
// like light flares spreading in multiple directions, eventually
// converging through the nova finalization process.
type DAG interface {
	// Add a new vertex to the DAG
	Add(ctx context.Context, vertex Vertex) error

	// Vote for vertex preferences
	Vote(ctx context.Context, vertexID ids.ID) error

	// RecordPoll records the result of photon sampling
	RecordPoll(ctx context.Context, votes map[ids.ID]int) error

	// Preferred returns current preferred vertices
	Preferred() set.Set[ids.ID]

	// Virtuous returns virtuous vertices (no conflicts)
	Virtuous() set.Set[ids.ID]

	// Conflicts returns conflicting vertex sets
	Conflicts(vertexID ids.ID) set.Set[ids.ID]

	// IsVirtuous checks if a vertex is virtuous
	IsVirtuous(vertexID ids.ID) bool

	// HealthCheck returns DAG health metrics
	HealthCheck(ctx context.Context) (Health, error)
}

// Vertex represents a vertex in the flare DAG
// (Previously avalanche.Vertex)
type Vertex interface {
	avalanche.Vertex

	// FlareHeight returns the vertex's height in the DAG
	FlareHeight() uint64

	// FlareScore returns the vertex's consensus score
	FlareScore() uint64

	// Photons returns the number of photon queries
	Photons() int

	// ConflictSet returns the conflict set this vertex belongs to
	ConflictSet() ids.ID
}

// FlareState represents the current state of DAG consensus
type FlareState struct {
	PreferredSet      set.Set[ids.ID]
	VirtuousSet       set.Set[ids.ID]
	ProcessingSet     set.Set[ids.ID]
	ConflictingSet    set.Set[ids.ID]
	OutstandingVertex int
	FlareIntensity    int // Overall DAG consensus strength (0-100)
}

// Health represents DAG consensus health
type Health struct {
	Healthy            bool
	FlareCoherence     float64   // 0-1, how coherent the DAG is
	ConflictRatio      float64   // Ratio of conflicting to total vertices
	VirtuousRatio      float64   // Ratio of virtuous to total vertices
	LastPollTime       time.Time
	OutstandingVertex  int
	ConflictSets       int
}

// ConflictSet represents a set of conflicting vertices
// Only one vertex from each conflict set can be finalized
type ConflictSet struct {
	ID       ids.ID
	Vertices set.Set[ids.ID]
	Leader   ids.ID // Current preferred vertex in set
}

// FlareMetrics tracks DAG consensus performance
type FlareMetrics struct {
	VerticesAdded      uint64
	VerticesFinalized  uint64
	ConflictSetsFormed uint64
	ConflictsResolved  uint64
	PollsProcessed     uint64
	AveragePollTime    time.Duration
}
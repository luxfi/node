// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/engine/core/tracker"
	"github.com/luxfi/node/v2/quasar/engine/dag/bootstrap/queue"
	"github.com/luxfi/node/v2/quasar/engine/dag/vertex"
	"github.com/luxfi/node/v2/quasar/networking/sender"
)

// Bootstrapper bootstraps a DAG
type Bootstrapper interface {
	core.Engine

	// Initialize initializes the bootstrapper
	Initialize(Config) error

	// CurrentAcceptedFrontier returns the current accepted frontier
	CurrentAcceptedFrontier(context.Context) []ids.ID

	// FilterAccepted returns the accepted vertex IDs from the provided set
	FilterAccepted(context.Context, []ids.ID) []ids.ID

	// Add adds a vertex to the bootstrapper
	Add(context.Context, vertex.Vertex) error

	// GetMissingVertices returns vertices that are missing
	GetMissingVertices(context.Context) ([]ids.ID, error)

	// ForceAccepted forces a vertex to be accepted
	ForceAccepted(context.Context, []ids.ID) error
}

// Config configures the bootstrapper
type Config struct {
	// Context provides engine context
	Context *core.Context

	// StartupTracker tracks startup progress
	StartupTracker tracker.StartupTracker

	// Sender sends consensus messages
	Sender sender.Sender

	// AncestorsMaxContainersReceived is the maximum number of containers to receive from a peer
	AncestorsMaxContainersReceived int

	// VtxBlocked tracks operations that are blocked on vertices
	VtxBlocked *queue.JobsWithMissing

	// TxBlocked tracks operations that are blocked on transactions
	TxBlocked *queue.Jobs

	// Manager manages the DAG vertices
	Manager vertex.Manager

	// VM is the VM that the consensus engine is running on
	VM vertex.LinearizableVM
}

// Parameters configures bootstrapping behavior
type Parameters struct {
	// ShouldBootstrap returns true if bootstrapping should be performed
	ShouldBootstrap bool

	// NumAcceptedRequired is the number of vertices that must be accepted before bootstrapping is considered complete
	NumAcceptedRequired uint64

	// NumAcceptedStarting is the number of accepted vertices at the start of bootstrapping
	NumAcceptedStarting uint64

	// AcceptedFrontierRequestTimeout is the timeout for accepted frontier requests
	AcceptedFrontierRequestTimeout time.Duration
}
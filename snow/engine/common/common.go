// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/choices"
)

// Block defines the common interface for all blocks
type Block interface {
	Decidable

	// Parent returns the ID of this block's parent
	Parent() ids.ID

	// Height returns the height of this block
	Height() uint64

	// Timestamp returns the time this block was created
	Timestamp() uint64

	// Verify that the state transition this block would make is valid
	Verify() error

	// Bytes returns the binary representation of this block
	Bytes() []byte
}

// Decidable represents an element that can be decided
type Decidable interface {
	// ID returns the unique ID of this element
	ID() ids.ID

	// Accept accepts this element and changes its status to Accepted
	Accept() error

	// Reject rejects this element and changes its status to Rejected
	Reject() error

	// Status returns the current status
	Status() choices.Status
}

// Vertex defines the common interface for all vertices
type Vertex interface {
	Decidable

	// Parents returns the IDs of this vertex's parents
	Parents() []ids.ID

	// Height returns the height of this vertex
	Height() uint64

	// Epoch returns the epoch this vertex was created in
	Epoch() uint32

	// Verify that the state transition this vertex would make is valid
	Verify() error

	// Bytes returns the binary representation of this vertex
	Bytes() []byte
}

// Engine defines the common interface for consensus engines
type Engine interface {
	// Start the engine
	Start(uint64) error

	// Stop the engine
	Stop() error

	// HealthCheck returns nil if the engine is healthy
	HealthCheck() (interface{}, error)
}

// Handler defines the interface for handling consensus messages
type Handler interface {
	Engine

	// SetState sets the state of the handler
	SetState(EngineState) error

	// GetState returns the current state
	GetState() EngineState
}

// EngineState represents the state of a consensus engine
type EngineState uint32

const (
	// Initializing state
	Initializing EngineState = iota
	// Bootstrapping state
	Bootstrapping
	// NormalOp state - normal operation
	NormalOp
	// Stopped state
	Stopped
)

// String returns the string representation of the engine state
func (s EngineState) String() string {
	switch s {
	case Initializing:
		return "Initializing"
	case Bootstrapping:
		return "Bootstrapping"
	case NormalOp:
		return "NormalOp"
	case Stopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

// Sender defines the interface for sending consensus messages
type Sender interface {
	// SendGetAcceptedFrontier sends a GetAcceptedFrontier message
	SendGetAcceptedFrontier(nodeID ids.NodeID, requestID uint32)

	// SendAcceptedFrontier sends an AcceptedFrontier message
	SendAcceptedFrontier(nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)

	// SendGetAccepted sends a GetAccepted message
	SendGetAccepted(nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)

	// SendAccepted sends an Accepted message
	SendAccepted(nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID)

	// SendGet sends a Get message
	SendGet(nodeID ids.NodeID, requestID uint32, containerID ids.ID)

	// SendPut sends a Put message
	SendPut(nodeID ids.NodeID, requestID uint32, container []byte)

	// SendPushQuery sends a PushQuery message
	SendPushQuery(nodeIDs []ids.NodeID, requestID uint32, container []byte)

	// SendPullQuery sends a PullQuery message
	SendPullQuery(nodeIDs []ids.NodeID, requestID uint32, containerID ids.ID)

	// SendChits sends a Chits message
	SendChits(nodeID ids.NodeID, requestID uint32, votes []ids.ID)
}

// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"time"

	"github.com/luxfi/node/v2/message"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/networking/router"
	"github.com/luxfi/node/v2/quasar/validators"
)

// Handler handles incoming consensus messages
type Handler interface {
	// Context returns the handler's context
	Context() *core.Context

	// SetEngine sets the consensus engine
	SetEngine(engine core.Engine)

	// Push pushes a message to be handled
	Push(context.Context, Message) error

	// Len returns the number of pending messages
	Len() int

	// Stop stops the handler
	Stop(context.Context)
}

// Message represents a consensus message
type Message struct {
	// InboundMessage is the message received from the network
	message.InboundMessage
	// Engine is the consensus engine to handle the message
	Engine core.Engine
	// Received is when the message was received
	Received time.Time
}

// Config configures the consensus message handler
type Config struct {
	Ctx                 *core.Context
	Validators          validators.Set
	MsgFromVMChan       <-chan message.InboundMessage
	ConsensusParams     Parameters
	Gossiper            Gossiper
	ExternalGossiper    ExternalGossiper
	Router              router.Router
}

// Parameters configures consensus handling behavior
type Parameters struct {
	K                       int
	AlphaPreference         int
	AlphaConfidence         int
	Beta                    int
	ConcurrentRepolls       int
	OptimalProcessing       int
	MaxOutstandingItems     int
	MaxItemProcessingTime   time.Duration
}

// Gossiper handles gossip operations
type Gossiper interface {
	// Gossip sends a gossip message
	Gossip(context.Context) error
}

// ExternalGossiper handles external gossip operations
type ExternalGossiper interface {
	Gossiper
	// GossipExternal sends an external gossip message
	GossipExternal(context.Context) error
}
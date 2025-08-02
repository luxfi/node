// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/message"
	"github.com/luxfi/node/v2/quasar/validators"
	"github.com/luxfi/node/v2/version"
)

// Engine describes the standard interface of a consensus engine.
type Engine interface {
	Handler

	// GetVM returns the underlying VM.
	GetVM() interface{}
}

// ChainVM is a VM that can be used by a consensus engine.
type ChainVM interface {
	// Initialize initializes the VM.
	Initialize(ctx context.Context, chainCtx *Context, db interface{}, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgChan chan Message, fxs []*Fx, appSender AppSender) error

	// Connected is called when a node is connected.
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error

	// Disconnected is called when a node is disconnected.
	Disconnected(ctx context.Context, nodeID ids.NodeID) error

	// HealthCheck returns the health status of the VM.
	HealthCheck(context.Context) (interface{}, error)

	// Shutdown shuts down the VM.
	Shutdown(context.Context) error

	// CreateHandlers returns the handlers for the VM.
	CreateHandlers(context.Context) (map[string]interface{}, error)

	// SetState sets the state of the VM.
	SetState(ctx context.Context, state State) error

	// Version returns the version of the VM.
	Version(context.Context) (string, error)
}

// Handler defines the functions that are called when messages are received
// from the network.
type Handler interface {
	// Context returns the context this Handler is operating in.
	Context() *Context

	// Start the engine.
	Start(ctx context.Context, startReqID uint32) error

	// Stop the engine.
	Stop(ctx context.Context) error

	// Notify the engine that a new block is ready to be proposed.
	Notify(context.Context, Message) error

	// GetStateSummaryFrontier returns the state summary frontier.
	GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// StateSummaryFrontier is called when the state summary frontier is received.
	StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error

	// GetAcceptedStateSummary retrieves the state summary for the given block IDs.
	GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights []uint64) error

	// AcceptedStateSummary is called when the requested state summary is received.
	AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error

	// GetAcceptedFrontier returns the set of accepted frontier vertices.
	GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// AcceptedFrontier is called when the accepted frontier is received.
	AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error

	// GetAccepted returns the set of accepted vertices.
	GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// Accepted is called when the accepted set is received.
	Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// Get retrieves a container and its ancestors.
	Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error

	// GetAncestors retrieves a container and its ancestors.
	GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error

	// Put is called when a container is received.
	Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error

	// Ancestors is called when a container and its ancestors are received.
	Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error

	// PushQuery pushes a query to the given node.
	PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) error

	// PullQuery pulls a query from the given node.
	PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error

	// QueryFailed is called when a query fails.
	QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// Chits is called when chits are received.
	Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) error

	// AppRequest is called when an application request is received.
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error

	// AppResponse is called when an application response is received.
	AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error

	// AppGossip is called when an application gossip message is received.
	AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error

	// CrossChainAppRequest is called when a cross-chain application request is received.
	CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error

	// CrossChainAppResponse is called when a cross-chain application response is received.
	CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error

	// Connected is called when a node is connected.
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error

	// Disconnected is called when a node is disconnected.
	Disconnected(ctx context.Context, nodeID ids.NodeID) error

	// Health returns the health status of the engine.
	HealthCheck(context.Context) (interface{}, error)

	// Shutdown the engine.
	Shutdown(context.Context) error
}

// Context contains the state that is used by consensus engines.
type Context struct {
	// ChainID is the chain this engine is working on.
	ChainID ids.ID

	// SubnetID is the subnet this engine is working on.
	SubnetID ids.ID

	// NodeID is the ID of this node.
	NodeID ids.NodeID

	// Registerer for registering metrics.
	Registerer Registerer

	// Log is used for logging messages.
	Log Logger

	// Lock is used to synchronize access to shared resources.
	Lock sync.Locker

	// ValidatorSet contains the validators for this subnet.
	ValidatorSet validators.Set

	// ValidatorState provides access to validator information.
	ValidatorState ValidatorState

	// Sender is used to send messages to other nodes.
	Sender Sender

	// Bootstrappers are the nodes that are used to bootstrap this chain.
	Bootstrappers []ids.NodeID

	// StartTime is the time this engine started.
	StartTime time.Time

	// RequestID is used to create unique request IDs.
	RequestID *RequestID
}

// Message is an incoming message.
type Message struct {
	// Type is the type of message.
	Type message.Op

	// NodeID is the ID of the node that sent this message.
	NodeID ids.NodeID

	// The body of the message.
	Body interface{}
}

// Request represents a request.
type Request struct {
	// NodeID of the node this request was sent to.
	NodeID ids.NodeID

	// RequestID is the ID of this request.
	RequestID uint32

	// Deadline is the time by which this request must be fulfilled.
	Deadline time.Time

	// The handler to invoke when this request fails or succeeds.
	Handler Handler
}

// ValidatorState provides access to validator information
type ValidatorState interface {
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

// For missing imports
type (
	Registerer interface{}
	Logger     interface{}
	Sender     interface{}
	RequestID  struct{}
)

// VM is the interface all VMs must implement
type VM interface {
	// Initialize initializes the VM
	Initialize(
		ctx context.Context,
		chainCtx *Context,
		dbManager interface{},
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		toEngine chan<- Message,
		fxs []*Fx,
		appSender AppSender,
	) error

	// SetState sets the state of the VM
	SetState(ctx context.Context, state State) error

	// Shutdown shuts down the VM
	Shutdown(context.Context) error

	// Version returns the version of the VM
	Version(context.Context) (string, error)

	// CreateHandlers returns the handlers for the VM
	CreateHandlers(context.Context) (map[string]interface{}, error)

	// CreateStaticHandlers returns the static handlers for the VM
	CreateStaticHandlers(context.Context) (map[string]interface{}, error)

	// HealthCheck returns the health of the VM
	HealthCheck(context.Context) (interface{}, error)

	// Connected is called when a peer is connected
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error

	// Disconnected is called when a peer is disconnected
	Disconnected(ctx context.Context, nodeID ids.NodeID) error

	// AppRequest handles an application request
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error

	// AppResponse handles an application response
	AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error

	// AppGossip handles an application gossip message
	AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error

	// CrossChainAppRequest handles a cross-chain application request
	CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error

	// CrossChainAppResponse handles a cross-chain application response
	CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error
}

// State represents the current state of the VM
type State uint8

const (
	// StateBootstrapping is the state when the VM is bootstrapping
	StateBootstrapping State = iota
	// StateConsensus is the state when the VM is in consensus
	StateConsensus
)

// Fx represents a feature extension
type Fx struct {
	ID ids.ID
	Fx interface{}
}

// AppSender sends application-level messages
type AppSender interface {
	// SendAppRequest sends an application request
	SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, msg []byte) error

	// SendAppResponse sends an application response
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error

	// SendAppError sends an application error response
	SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error

	// SendAppGossip sends an application gossip message
	SendAppGossip(ctx context.Context, msg []byte) error

	// SendCrossChainAppRequest sends a cross-chain application request
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error

	// SendCrossChainAppResponse sends a cross-chain application response
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error
}

// AppError represents an application error
type AppError struct {
	Code    int32
	Message string
}

func (e AppError) Error() string {
	return e.Message
}

// Common application errors
var (
	ErrUndefined = &AppError{
		Code:    -1,
		Message: "undefined error",
	}
	ErrTimeout = &AppError{
		Code:    -2,
		Message: "timeout",
	}
)

// SendConfig configures sending behavior
type SendConfig struct {
	Validators []ids.NodeID
	NodeIDs    []ids.NodeID
	Peers      int
}

// PendingTxs represents pending transactions
type PendingTxs struct {
	Txs [][]byte
}

// BootstrapTracker tracks bootstrap progress
type BootstrapTracker interface {
	// IsBootstrapped returns true if the chain is bootstrapped
	IsBootstrapped() bool

	// Bootstrapped marks the chain as bootstrapped
	Bootstrapped()

	// OnValidatorAdded is called when a validator is added
	OnValidatorAdded(nodeID ids.NodeID, weight uint64)

	// OnValidatorRemoved is called when a validator is removed
	OnValidatorRemoved(nodeID ids.NodeID, weight uint64)

	// OnValidatorWeightChanged is called when a validator's weight changes
	OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64)
}

// PreemptionSignal is used to signal preemption
type PreemptionSignal struct {
	once sync.Once
	done chan struct{}
}

// NewPreemptionSignal creates a new preemption signal
func NewPreemptionSignal() *PreemptionSignal {
	return &PreemptionSignal{
		done: make(chan struct{}),
	}
}

// Listen returns a channel that will be closed when Preempt is called
func (p *PreemptionSignal) Listen() <-chan struct{} {
	if p.done == nil {
		p.done = make(chan struct{})
	}
	return p.done
}

// Preempt triggers the signal by closing the channel
func (p *PreemptionSignal) Preempt() {
	p.once.Do(func() {
		if p.done != nil {
			close(p.done)
		}
	})
}

// NormalOp is a normal operation
const NormalOp = 0
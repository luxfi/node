// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/ids"
	"github.com/luxfi/metrics"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/quasar/consensus/beam/poll"
	"github.com/luxfi/node/utils/set"
)

// Engine implements the Beam consensus engine
type Engine struct {
	// Configuration
	config Config
	ctx    *quasar.ConsensusContext

	// State management
	state         quasar.State
	vm            VM
	blockBuilding bool

	// Consensus state
	preference    ids.ID
	lastAccepted  ids.ID
	polls         *poll.Set

	// Quantum security
	quasar        *Quasar
	slashChannel  chan SlashEvent

	// Metrics
	metrics       *beamMetrics

	// Channels
	incomingMsgs  chan message
	done          chan struct{}
}

// Config contains the configuration for the Beam engine
type Config struct {
	// Consensus parameters
	Params            Parameters
	StartupTracker    StartupTracker
	Sender            Sender
	Timer             Timer
	AncestorTracker   AncestorTracker

	// Quantum parameters
	QuasarEnabled     bool
	QuasarTimeout     time.Duration
	RingtailThreshold int
}

// Parameters contains consensus parameters
type Parameters struct {
	K                     int           // Sample size
	AlphaPreference       int           // Preference threshold
	AlphaConfidence       int           // Confidence threshold
	Beta                  int           // Decision threshold
	MaxItemProcessingTime time.Duration // Max time to process an item
}

// VM is the interface for the virtual machine
type VM interface {
	// GetBlock returns the block with the given ID
	GetBlock(context.Context, ids.ID) (quasar.Block, error)

	// BuildBlock builds a new block
	BuildBlock(context.Context) (quasar.Block, error)

	// SetPreference sets the preferred block
	SetPreference(context.Context, ids.ID) error

	// LastAccepted returns the last accepted block
	LastAccepted(context.Context) (ids.ID, error)

	// VerifyWithContext verifies a block with context
	VerifyWithContext(context.Context, quasar.Block) error
}

// Sender sends consensus messages
type Sender interface {
	// SendGetAncestors sends a GetAncestors message
	SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error

	// SendPullQuery sends a PullQuery message
	SendPullQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerID ids.ID) error

	// SendPushQuery sends a PushQuery message
	SendPushQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, container []byte) error

	// SendChits sends a Chits message
	SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, acceptedID ids.ID) error
}

// Timer manages timeouts
type Timer interface {
	// SetTimeout sets a timeout
	SetTimeout(duration time.Duration) error

	// Cancel cancels the current timeout
	Cancel() error
}

// StartupTracker tracks if the node is started
type StartupTracker interface {
	Started() bool
}

// AncestorTracker tracks block ancestors
type AncestorTracker interface {
	// Add adds a block to track
	Add(blkID ids.ID) error

	// Remove removes a block from tracking
	Remove(blkID ids.ID) error

	// IsAncestor returns true if ancestor is an ancestor of descendant
	IsAncestor(ancestor, descendant ids.ID) (bool, error)
}

// SlashEvent represents a slashing event
type SlashEvent struct {
	ProposerID ids.NodeID
	Height     uint64
	Reason     string
	Timestamp  time.Time
}

// message is an internal message
type message struct {
	msgType   messageType
	nodeID    ids.NodeID
	requestID uint32
	container []byte
}

type messageType int

const (
	_ messageType = iota
	getAncestorsMsg
	pullQueryMsg
	pushQueryMsg
	chitsMsg
)

// NewEngine creates a new Beam consensus engine
func NewEngine(
	config Config,
	ctx *quasar.ConsensusContext,
	vm VM,
) (*Engine, error) {
	if err := config.Params.Valid(); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	e := &Engine{
		config:       config,
		ctx:          ctx,
		vm:           vm,
		state:        quasar.NewMemoryState(),
		polls:        poll.NewSet(),
		slashChannel: make(chan SlashEvent, 10),
		incomingMsgs: make(chan message, 1000),
		done:         make(chan struct{}),
	}

	// Initialize metrics
	// Create a metrics instance using the prometheus registerer
	var metricsInstance metrics.Metrics
	if promReg, ok := ctx.Registerer.(*prometheus.Registry); ok {
		metricsInstance = metrics.NewPrometheusMetrics("quasar", promReg)
	} else {
		// Fallback to no-op metrics if not a prometheus registry
		metricsInstance = metrics.NewNoOpMetrics("quasar")
	}
	e.metrics = newMetrics(metricsInstance)

	// Initialize Quasar if enabled
	var err error
	if config.QuasarEnabled {
		quasarConfig := QuasarConfig{
			Threshold:     config.RingtailThreshold,
			QuasarTimeout: config.QuasarTimeout,
			Validators:    ctx.ValidatorState,
		}
		e.quasar, err = NewQuasar(ctx.NodeID, ctx.RingtailSK, quasarConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create quasar: %w", err)
		}
	}

	return e, nil
}

// Start starts the consensus engine
func (e *Engine) Start(ctx context.Context) error {
	e.ctx.Log.Info("starting Beam consensus engine",
		"quasarEnabled", e.config.QuasarEnabled,
	)

	// Get last accepted block
	lastAcceptedID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted: %w", err)
	}
	e.lastAccepted = lastAcceptedID
	e.preference = lastAcceptedID

	// Start message processing
	go e.processMessages()

	return nil
}

// Stop stops the consensus engine
func (e *Engine) Stop() error {
	close(e.done)
	return nil
}

// GetSlashChannel returns the slash event channel
func (e *Engine) GetSlashChannel() <-chan SlashEvent {
	return e.slashChannel
}

// BuildBlock builds a new block
func (e *Engine) BuildBlock(ctx context.Context) (quasar.Block, error) {
	if e.blockBuilding {
		return nil, errors.New("block building already in progress")
	}
	e.blockBuilding = true
	defer func() { e.blockBuilding = false }()

	// Build block via VM
	block, err := e.vm.BuildBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build block: %w", err)
	}

	// If Quasar is enabled, add dual certificates
	if e.config.QuasarEnabled {
		if err := e.addDualCertificates(ctx, block); err != nil {
			return nil, fmt.Errorf("failed to add dual certificates: %w", err)
		}
	}

	return block, nil
}

// addDualCertificates adds BLS and Ringtail certificates to a block
func (e *Engine) addDualCertificates(ctx context.Context, block quasar.Block) error {
	quasarBlock, ok := block.(quasar.QuasarBlock)
	if !ok {
		return errors.New("block does not support dual certificates")
	}

	// Sign with BLS
	blsSig, err := e.signBLS(block.Bytes())
	if err != nil {
		return fmt.Errorf("failed to sign with BLS: %w", err)
	}

	// Get Ringtail certificate with timeout
	rtCert, err := e.getRingtailCertificate(ctx, block)
	if err != nil {
		// Slash proposer for missing RT certificate
		e.slashChannel <- SlashEvent{
			ProposerID: e.ctx.NodeID,
			Height:     block.Height(),
			Reason:     "Quasar timeout: missing Ringtail certificate",
			Timestamp:  time.Now(),
		}
		return fmt.Errorf("failed to get Ringtail certificate: %w", err)
	}

	// Attach certificates
	if err := attachCertificates(quasarBlock, blsSig, rtCert); err != nil {
		return fmt.Errorf("failed to attach certificates: %w", err)
	}

	return nil
}

// signBLS signs data with BLS
func (e *Engine) signBLS(data []byte) ([]byte, error) {
	// TODO: Implement actual BLS signing
	return make([]byte, 96), nil
}

// getRingtailCertificate gets a Ringtail certificate for a block
func (e *Engine) getRingtailCertificate(ctx context.Context, block quasar.Block) ([]byte, error) {
	if e.quasar == nil {
		return nil, errors.New("Quasar not initialized")
	}

	// Create certificate channel
	certCh := make(chan []byte, 1)

	// Register for certificate
	e.quasar.RegisterForCertificate(block.Height(), certCh)

	// Wait with timeout
	timer := time.NewTimer(e.config.QuasarTimeout)
	defer timer.Stop()

	select {
	case cert := <-certCh:
		return cert, nil
	case <-timer.C:
		return nil, errors.New("Quasar timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// attachCertificates attaches certificates to a block
func attachCertificates(block quasar.QuasarBlock, blsSig, rtCert []byte) error {
	// TODO: Implement certificate attachment based on block type
	return nil
}

// processMessages processes incoming consensus messages
func (e *Engine) processMessages() {
	for {
		select {
		case msg := <-e.incomingMsgs:
			if err := e.handleMessage(msg); err != nil {
				e.ctx.Log.Debug("failed to handle message",
					"type", msg.msgType,
					"nodeID", msg.nodeID,
					"error", err,
				)
			}
		case <-e.done:
			return
		}
	}
}

// handleMessage handles a consensus message
func (e *Engine) handleMessage(msg message) error {
	switch msg.msgType {
	case getAncestorsMsg:
		return e.handleGetAncestors(msg.nodeID, msg.requestID, msg.container)
	case pullQueryMsg:
		return e.handlePullQuery(msg.nodeID, msg.requestID, msg.container)
	case pushQueryMsg:
		return e.handlePushQuery(msg.nodeID, msg.requestID, msg.container)
	case chitsMsg:
		return e.handleChits(msg.nodeID, msg.requestID, msg.container)
	default:
		return fmt.Errorf("unknown message type: %v", msg.msgType)
	}
}

// Valid returns true if the parameters are valid
func (p Parameters) Valid() error {
	if p.K <= 0 {
		return errors.New("K must be positive")
	}
	if p.AlphaPreference <= 0 || p.AlphaPreference > p.K {
		return errors.New("AlphaPreference must be in (0, K]")
	}
	if p.AlphaConfidence <= 0 || p.AlphaConfidence > p.K {
		return errors.New("AlphaConfidence must be in (0, K]")
	}
	if p.Beta <= 0 {
		return errors.New("Beta must be positive")
	}
	return nil
}

// Placeholder implementations for message handlers
func (e *Engine) handleGetAncestors(nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (e *Engine) handlePullQuery(nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (e *Engine) handlePushQuery(nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (e *Engine) handleChits(nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}
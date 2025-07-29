// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/quasar/validators"
)

// ConsensusContext is the minimum information a consensus engine needs to run
type ConsensusContext struct {
	// PrimaryAlias is the primary alias of the chain this context is for
	PrimaryAlias string

	// Registerer is used to register metrics
	Registerer prometheus.Registerer

	// Tracer is used to trace consensus operations (placeholder for now)
	// Tracer trace.Tracer

	// Context is the context this consensus is running in
	*Context

	// BlockAcceptor is called when a block is accepted
	BlockAcceptor Acceptor
	// TxAcceptor is called when a transaction is accepted
	TxAcceptor Acceptor
	// VertexAcceptor is called when a vertex is accepted
	VertexAcceptor Acceptor
}

// Context is the shared context for Quasar consensus
type Context struct {

	// NetworkID is the ID of the network this node is connected to
	NetworkID uint32
	// SubnetID is the ID of the subnet this node validates
	SubnetID ids.ID
	// ChainID is the ID of the chain this node validates
	ChainID ids.ID
	// NodeID is the ID of this node
	NodeID ids.NodeID
	// PublicKey is the BLS public key of this node
	PublicKey *bls.PublicKey

	// XChainID is the ID of the X-Chain
	XChainID ids.ID
	// CChainID is the ID of the C-Chain
	CChainID ids.ID
	// LUXAssetID is the ID of the LUX asset
	LUXAssetID ids.ID

	// Log is the logger for this consensus
	Log log.Logger
	// Lock is a general-purpose lock for consensus
	Lock sync.RWMutex

	// Keystore is the keystore for this node
	Keystore Keystore
	// SharedMemory is the shared memory for cross-chain communication
	SharedMemory SharedMemory
	// BCLookup maps aliases to chain IDs
	BCLookup BCLookup
	// Metrics is the metrics registry
	Metrics prometheus.Gatherer

	// SubnetTracker tracks subnet membership
	SubnetTracker SubnetTracker

	// ValidatorState is the validator set state
	// The primary network's validator set is the union of all subnets' validator sets
	ValidatorState validators.State

	// ChainDataDir is the directory where chain data is stored
	ChainDataDir string

	// Quantum-specific fields
	QuasarEnabled bool
	RingtailSK    []byte // Ringtail secret key
	RingtailPK    []byte // Ringtail public key
}

// IsBootstrapped returns true if the chain is done bootstrapping
func (ctx *ConsensusContext) IsBootstrapped() bool {
	// TODO: Implement proper state tracking
	return true
}

// Acceptor is a function that is called when an element is accepted
type Acceptor interface {
	Accept(*ConsensusContext, ids.ID, []byte) error
}

// SharedMemory is the interface for shared memory
type SharedMemory interface {
	Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
	Indexed(peerChainID ids.ID, values [][]byte) error
}

// BCLookup is the interface for looking up chain IDs by alias
type BCLookup interface {
	Lookup(alias string) (ids.ID, error)
	PrimaryAlias(id ids.ID) (string, error)
}

// Keystore is the interface for the keystore
type Keystore interface {
	GetUser(username string) (string, error)
	AddUser(username string, password string) error
}

// SubnetTracker tracks subnet membership
type SubnetTracker interface {
	Tracked(subnetID ids.ID) bool
	OnFinishedBootstrapping(subnetID ids.ID) chan struct{}
}

// EngineType is the type of consensus engine
type EngineType uint8

const (
	// EngineTypeUnknown is an unknown engine type
	EngineTypeUnknown EngineType = iota
	// EngineTypeBeam is the Beam linear consensus engine
	EngineTypeBeam
	// EngineTypeNova is the Nova DAG consensus engine
	EngineTypeNova
	// EngineTypeQuasar is the Quasar quantum-secure engine
	EngineTypeQuasar
)

// EngineStateType is the state of the consensus engine
type EngineStateType uint8

const (
	// Bootstrapping means the engine is bootstrapping
	Bootstrapping EngineStateType = iota
	// NormalOp means the engine is operating normally
	NormalOp
)

// EngineState is the state of a consensus engine
type EngineState struct {
	Type  EngineType
	State EngineStateType
}

// EngineStateManager manages consensus state
type EngineStateManager struct {
	mu    sync.RWMutex
	state EngineState
}

// Get returns the current state
func (s *EngineStateManager) Get() EngineState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Set sets the current state
func (s *EngineStateManager) Set(state EngineState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}
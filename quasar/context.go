// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metrics"

	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/quasar/validators"
)

var (
	// ErrNotFound is returned when a requested item is not found
	ErrNotFound = errors.New("not found")
)

// Sender sends consensus messages
type Sender interface {
	// SendGetAcceptedFrontier sends a GetAcceptedFrontier message
	SendGetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// SendAcceptedFrontier sends an AcceptedFrontier message
	SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// SendGetAccepted sends a GetAccepted message
	SendGetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// SendAccepted sends an Accepted message
	SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error

	// SendGet sends a Get message  
	SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error

	// SendPut sends a Put message
	SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error

	// SendPushQuery sends a PushQuery message
	SendPushQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, container []byte) error

	// SendPullQuery sends a PullQuery message
	SendPullQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerID ids.ID) error

	// SendChits sends a Chits message
	SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, acceptedID ids.ID) error
}

// Context is an alias for ConsensusContext
type Context = ConsensusContext

// ConsensusContext provides the context needed for consensus engines
type ConsensusContext struct {
	// NetworkID is the ID of the network this node is running on
	NetworkID uint32

	// ChainID is the ID of the chain this consensus engine is running on
	ChainID ids.ID

	// SubnetID is the ID of the subnet this chain belongs to
	SubnetID ids.ID

	// NodeID is the ID of this node
	NodeID ids.NodeID

	// Log is the logger for this consensus engine
	Log log.Logger

	// Metrics registry for this consensus engine
	Metrics metrics.Registry

	// Network sender for consensus messages
	Sender Sender

	// Validators manager
	Validators validators.Manager

	// Current time provider
	Clock Clock

	// Consensus parameters
	Parameters Parameters

	// Metrics registerer
	Registerer metrics.Registerer

	// Validator state
	ValidatorState validators.State

	// Ringtail secret key
	RingtailSK []byte

	// Ringtail public key
	RingtailPK []byte

	// Nested context for advanced operations
	Context *Context

	// BCLookup provides blockchain lookup functionality
	BCLookup ids.AliaserReader

	// Lock provides synchronization for the consensus engine
	Lock sync.RWMutex

	// State provides the chain state management
	State *EngineState

	// LUXAssetID is the asset ID for LUX
	LUXAssetID ids.ID

	// SharedMemory for cross-chain communication
	SharedMemory SharedMemory

	// WarpSigner for warp message signing
	WarpSigner WarpSigner

	// NetworkUpgrades configuration
	NetworkUpgrades NetworkUpgrades

	// PublicKey of this node
	PublicKey []byte

	// XChainID is the ID of the X-Chain
	XChainID ids.ID

	// CChainID is the ID of the C-Chain
	CChainID ids.ID

	// ChainDataDir is the directory for chain data
	ChainDataDir string

	// ValidatorSet provides access to the current validator set
	ValidatorSet ValidatorSet

	// Bootstrappers is the set of nodes to bootstrap from
	Bootstrappers validators.Set

	// StartTime is the time the consensus engine started
	StartTime time.Time

	// RequestID for tracking requests
	RequestID RequestID
}



// WarpSigner provides warp message signing functionality
type WarpSigner interface {
	// Sign signs a warp message
	Sign(msg *WarpMessage) (*WarpSignature, error)
}

// WarpMessage represents a warp message
type WarpMessage struct {
	// Message fields would go here
}

// WarpSignature represents a warp signature
type WarpSignature struct {
	// Signature fields would go here
}

// NetworkUpgrades represents network upgrade configuration
type NetworkUpgrades interface {
	// IsActivated checks if an upgrade is activated at a given time
	IsActivated(upgradeTime time.Time) bool
}

// Clock provides time functionality
type Clock interface {
	Time() time.Time
}

// ValidatorSet provides access to the validator set
type ValidatorSet interface {
	// GetValidatorSet returns the validator set at a given height
	GetValidatorSet(height uint64) (validators.Set, error)
}

// RequestID represents a request identifier
type RequestID struct {
	// Fields for request tracking
}

// Logger creates a logger from a base logger
type Logger func(log.Logger) log.Logger

// Registerer creates a metrics registerer
type Registerer func(metrics.Registry) metrics.Registry

// Parameters holds consensus parameters
type Parameters struct {
	// K is the number of consecutive successful polls required for finalization
	K int

	// Alpha is the required percentage of stake to consider a poll successful
	Alpha int

	// Beta is the number of polls with no progress before declaring the block stuck
	Beta int

	// ConcurrentRepolls is the number of concurrent polls to run
	ConcurrentRepolls int

	// OptimalProcessing is the optimal number of processing items
	OptimalProcessing int

	// MaxOutstandingItems is the maximum number of outstanding items
	MaxOutstandingItems int

	// MaxItemProcessingTime is the maximum time to process an item
	MaxItemProcessingTime time.Duration
}

// NewMemoryStore creates a new in-memory block store
func NewMemoryStore() BlockStore {
	return &memoryStore{
		blocks: make(map[ids.ID]interface{}),
	}
}

// BlockStore manages block storage
type BlockStore interface {
	// GetBlock retrieves a block by ID
	GetBlock(id ids.ID) (interface{}, error)

	// PutBlock stores a block
	PutBlock(id ids.ID, block interface{}) error

	// DeleteBlock removes a block
	DeleteBlock(id ids.ID) error
}

// memoryStore is an in-memory implementation of BlockStore
type memoryStore struct {
	blocks map[ids.ID]interface{}
}

func (s *memoryStore) GetBlock(id ids.ID) (interface{}, error) {
	block, ok := s.blocks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return block, nil
}

func (s *memoryStore) PutBlock(id ids.ID, block interface{}) error {
	s.blocks[id] = block
	return nil
}

func (s *memoryStore) DeleteBlock(id ids.ID) error {
	delete(s.blocks, id)
	return nil
}
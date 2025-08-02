// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/utils/set"
)

var _ Subnet = (*subnet)(nil)

type Allower interface {
	// IsAllowed filters out nodes that are not allowed to connect to this subnet
	IsAllowed(nodeID ids.NodeID, isValidator bool) bool
}

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	core.BootstrapTracker

	// AddChain adds a chain to this Subnet
	AddChain(chainID ids.ID) bool

	// Config returns config of this Subnet
	Config() Config

	// AllBootstrapped returns a channel that is closed when all chains are bootstrapped
	AllBootstrapped() <-chan struct{}

	Allower
}

type subnet struct {
	lock            sync.RWMutex
	bootstrapping   set.Set[ids.ID]
	bootstrapped    set.Set[ids.ID]
	config          Config
	myNodeID        ids.NodeID
	bootstrapSignal *core.PreemptionSignal
}

func New(myNodeID ids.NodeID, config Config) Subnet {
	return &subnet{
		config:          config,
		myNodeID:        myNodeID,
		bootstrapSignal: core.NewPreemptionSignal(),
	}
}

func (s *subnet) AllBootstrapped() <-chan struct{} {
	return s.bootstrapSignal.Listen()
}

func (s *subnet) IsBootstrapped() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.bootstrapping.Len() == 0
}

// Bootstrapped implements the BootstrapTracker interface
func (s *subnet) Bootstrapped() {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Mark all currently bootstrapping chains as bootstrapped
	for chainID := range s.bootstrapping {
		s.bootstrapped.Add(chainID)
	}
	s.bootstrapping.Clear()
	s.bootstrapSignal.Preempt()
}

// BootstrappedChain marks a specific chain as bootstrapped
func (s *subnet) BootstrappedChain(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Remove(chainID)
	s.bootstrapped.Add(chainID)
	if s.bootstrapping.Len() > 0 {
		return
	}

	s.bootstrapSignal.Preempt()
}

func (s *subnet) AddChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.bootstrapping.Contains(chainID) || s.bootstrapped.Contains(chainID) {
		return false
	}

	s.bootstrapping.Add(chainID)
	return true
}

func (s *subnet) Config() Config {
	return s.config
}

// OnValidatorAdded implements the BootstrapTracker interface
func (s *subnet) OnValidatorAdded(nodeID ids.NodeID, weight uint64) {
	// No-op for subnet level tracking
}

// OnValidatorRemoved implements the BootstrapTracker interface
func (s *subnet) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	// No-op for subnet level tracking
}

// OnValidatorWeightChanged implements the BootstrapTracker interface
func (s *subnet) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	// No-op for subnet level tracking
}

func (s *subnet) IsAllowed(nodeID ids.NodeID, isValidator bool) bool {
	// Case 1: NodeID is this node
	// Case 2: This subnet is not validator-only subnet
	// Case 3: NodeID is a validator for this chain
	// Case 4: NodeID is explicitly allowed whether it's subnet validator or not
	return nodeID == s.myNodeID ||
		!s.config.ValidatorOnly ||
		isValidator ||
		s.config.AllowedNodes.Contains(nodeID)
}

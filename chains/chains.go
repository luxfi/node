// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"sync"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/nets"
)

var ErrNoPrimaryNetworkConfig = errors.New("no net config for primary network found")

// Nets holds the currently running chains on this node
type Nets struct {
	nodeID  ids.NodeID
	configs map[ids.ID]nets.Config

	lock   sync.RWMutex
	chains map[ids.ID]nets.Net
}

// GetOrCreate returns a chain running on this node, or creates one if it was
// not running before. Returns the chain and if the chain was created.
func (s *Nets) GetOrCreate(chainID ids.ID) (nets.Net, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if chain, ok := s.chains[chainID]; ok {
		return chain, false
	}

	// Default to the primary network config if a net config was not
	// specified
	config, ok := s.configs[chainID]
	if !ok {
		config = s.configs[constants.PrimaryNetworkID]
	}

	chain := nets.New(s.nodeID, config)
	s.chains[chainID] = chain

	return chain, true
}

// IsChainBootstrapped reports whether the given chain has finished initial sync
// (reached the network frontier and transitioned its VM to normal operation) on
// this node — Bootstrapped(chainID) was called for it in its validation net. A
// chain that is merely tracked (its sync goroutine launched) but has NOT converged
// reads false. This is the per-chain truth manager.IsBootstrapped / info.isBootstrapped
// key on, replacing the mere-existence test that returned true the instant a chain
// was added to the manager (the premature-true masking bug: a C-Chain stalled at
// genesis reported bootstrapped=true). A chainID is added to exactly one net's
// tracking, so the first net that reports it bootstrapped is authoritative.
func (s *Nets) IsChainBootstrapped(chainID ids.ID) bool {
	if s == nil {
		return false // no net tracking wired ⇒ nothing has been marked bootstrapped
	}
	s.lock.RLock()
	defer s.lock.RUnlock()

	for _, chain := range s.chains {
		if chain.IsChainBootstrapped(chainID) {
			return true
		}
	}
	return false
}

// Bootstrapping returns the chainIDs of any chains that are still
// bootstrapping.
func (s *Nets) Bootstrapping() []ids.ID {
	s.lock.RLock()
	defer s.lock.RUnlock()

	chainsBootstrapping := make([]ids.ID, 0, len(s.chains))
	for chainID, chain := range s.chains {
		if !chain.IsBootstrapped() {
			chainsBootstrapping = append(chainsBootstrapping, chainID)
		}
	}

	return chainsBootstrapping
}

// NewNets returns an instance of Nets
func NewNets(
	nodeID ids.NodeID,
	configs map[ids.ID]nets.Config,
) (*Nets, error) {
	if _, ok := configs[constants.PrimaryNetworkID]; !ok {
		return nil, ErrNoPrimaryNetworkConfig
	}

	s := &Nets{
		nodeID:  nodeID,
		configs: configs,
		chains:  make(map[ids.ID]nets.Net),
	}

	_, _ = s.GetOrCreate(constants.PrimaryNetworkID)
	return s, nil
}

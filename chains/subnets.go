// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/utils/constants"
)

var ErrNoPrimaryNetworkConfig = errors.New("no net config for primary network found")

// Subnets holds the currently running subnets on this node
type Subnets struct {
	nodeID  ids.NodeID
	configs map[ids.ID]nets.Config

	lock    sync.RWMutex
	subnets map[ids.ID]nets.Net
}

// GetOrCreate returns a subnet running on this node, or creates one if it was
// not running before. Returns the subnet and if the subnet was created.
func (s *Subnets) GetOrCreate(netID ids.ID) (nets.Net, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if subnet, ok := s.subnets[netID]; ok {
		return subnet, false
	}

	// Default to the primary network config if a net config was not
	// specified
	config, ok := s.configs[netID]
	if !ok {
		config = s.configs[constants.PrimaryNetworkID]
	}

	subnet := nets.New(s.nodeID, config)
	s.subnets[netID] = subnet

	return subnet, true
}

// Bootstrapping returns the netIDs of any chains that are still
// bootstrapping.
func (s *Subnets) Bootstrapping() []ids.ID {
	s.lock.RLock()
	defer s.lock.RUnlock()

	subnetsBootstrapping := make([]ids.ID, 0, len(s.subnets))
	for netID, subnet := range s.subnets {
		if !subnet.IsBootstrapped() {
			subnetsBootstrapping = append(subnetsBootstrapping, netID)
		}
	}

	return subnetsBootstrapping
}

// NewSubnets returns an instance of Subnets
func NewSubnets(
	nodeID ids.NodeID,
	configs map[ids.ID]nets.Config,
) (*Subnets, error) {
	if _, ok := configs[constants.PrimaryNetworkID]; !ok {
		return nil, ErrNoPrimaryNetworkConfig
	}

	s := &Subnets{
		nodeID:  nodeID,
		configs: configs,
		subnets: make(map[ids.ID]nets.Net),
	}

	_, _ = s.GetOrCreate(constants.PrimaryNetworkID)
	return s, nil
}

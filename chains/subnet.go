// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"sync"
	"time"

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/networking/sender"
=======
=======
>>>>>>> 53a8245a8 (Update consensus)
=======
>>>>>>> c5eafdb72 (Update LICENSE)
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/utils/set"
<<<<<<< HEAD
<<<<<<< HEAD
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
=======
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/networking/sender"
>>>>>>> 04d685aa2 (Update consensus)
>>>>>>> 53a8245a8 (Update consensus)
=======
>>>>>>> c5eafdb72 (Update LICENSE)
)

var _ Subnet = (*subnet)(nil)

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.Subnet

	afterBootstrapped() chan struct{}

	addChain(chainID ids.ID) bool
}

type SubnetConfig struct {
	sender.GossipConfig

	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	ValidatorOnly       bool                 `json:"validatorOnly" yaml:"validatorOnly"`
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	ConsensusParameters lux.Parameters `json:"consensusParameters" yaml:"consensusParameters"`
=======
=======
>>>>>>> 53a8245a8 (Update consensus)
=======
>>>>>>> c5eafdb72 (Update LICENSE)
	ConsensusParameters avalanche.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block.
	// TODO: Remove this flag once all VMs throttle their own block production.
	ProposerMinBlockDelay time.Duration `json:"proposerMinBlockDelay" yaml:"proposerMinBlockDelay"`
<<<<<<< HEAD
<<<<<<< HEAD
>>>>>>> c2bbcf98e (Add proposerMinBlockDelay to subnet config (#2202))
=======
=======
	ConsensusParameters lux.Parameters `json:"consensusParameters" yaml:"consensusParameters"`
>>>>>>> 04d685aa2 (Update consensus)
>>>>>>> 53a8245a8 (Update consensus)
=======
>>>>>>> c5eafdb72 (Update LICENSE)
}

type subnet struct {
	lock             sync.RWMutex
	bootstrapping    set.Set[ids.ID]
	bootstrapped     set.Set[ids.ID]
	once             sync.Once
	bootstrappedSema chan struct{}
}

func newSubnet() Subnet {
	return &subnet{
		bootstrappedSema: make(chan struct{}),
	}
}

func (s *subnet) IsBootstrapped() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.bootstrapping.Len() == 0
}

func (s *subnet) Bootstrapped(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Remove(chainID)
	s.bootstrapped.Add(chainID)
	if s.bootstrapping.Len() > 0 {
		return
	}

	s.once.Do(func() {
		close(s.bootstrappedSema)
	})
}

func (s *subnet) afterBootstrapped() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) addChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.bootstrapping.Contains(chainID) || s.bootstrapped.Contains(chainID) {
		return false
	}

	s.bootstrapping.Add(chainID)
	return true
}

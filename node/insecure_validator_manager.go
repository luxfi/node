// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/version"
)

// ExtendedManager extends the base validators.Manager with additional methods
type ExtendedManager interface {
	validators.Manager
	AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error
	RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error
}

type insecureValidatorManager struct {
	ChainRouter
	log    log.Logger
	vdrs   validators.Manager
	weight uint64
	// Keep track of added validators locally
	validators map[ids.ID]map[ids.NodeID]uint64
	mu         sync.RWMutex
}

func (i *insecureValidatorManager) Connected(vdrID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	if constants.PrimaryNetworkID == netID {
		// Track the validator locally since we can't modify the validators.Manager
		i.mu.Lock()
		if i.validators[netID] == nil {
			i.validators[netID] = make(map[ids.NodeID]uint64)
		}
		i.validators[netID][vdrID] = i.weight
		i.mu.Unlock()

		i.log.Debug("tracked validator connection",
			log.Stringer("nodeID", vdrID),
			log.Stringer("netID", netID),
			log.Uint64("weight", i.weight),
		)
	}
	// Forward to the underlying router
	if i.ChainRouter != nil {
		i.ChainRouter.Connected(vdrID, nodeVersion, netID)
	}
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.NodeID) {
	// RemoveWeight will only error here if there was an error reported during
	// Add.
	err := i.vdrs.RemoveWeight(constants.PrimaryNetworkID, vdrID, i.weight)
	if err != nil {
		i.log.Error("failed to remove weight",
			log.Stringer("nodeID", vdrID),
			log.Stringer("netID", constants.PrimaryNetworkID),
			log.Error(err),
		)
	}
	// Router.Disconnected not available in consensus package
	// i.Router.Disconnected(vdrID)
}
// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/constants"
	nodevalidators "github.com/luxfi/node/validators"
	"github.com/luxfi/node/version"
)

type insecureValidatorManager struct {
	Router
	log    log.Logger
	vdrs   nodevalidators.ExtendedManager
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
	i.Router.Connected(vdrID, nodeVersion, netID)
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.NodeID) {
	// Remove the validator from local tracking
	i.mu.Lock()
	if i.validators[constants.PrimaryNetworkID] != nil {
		delete(i.validators[constants.PrimaryNetworkID], vdrID)
	}
	i.mu.Unlock()

	i.log.Debug("removed validator",
		log.Stringer("nodeID", vdrID),
		log.Stringer("netID", constants.PrimaryNetworkID),
	)

	// Forward to the underlying router
	i.Router.Disconnected(vdrID)
}

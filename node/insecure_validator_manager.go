// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/luxfi/log"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/version"
)

type insecureValidatorManager struct {
	ChainRouter
	log    log.Logger
	vdrs   validators.Manager
	weight uint64
}

func (i *insecureValidatorManager) Connected(vdrID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	if constants.PrimaryNetworkID == netID {
		// Sybil protection is disabled so we don't have a txID that added the
		// peer as a validator. Because each validator needs a txID associated
		// with it, we hack one together by padding the nodeID with zeroes.
		dummyTxID := ids.Empty
		copy(dummyTxID[:], vdrID.Bytes())

		err := i.vdrs.AddStaker(constants.PrimaryNetworkID, vdrID, nil, dummyTxID, i.weight)
		if err != nil {
			i.log.Error("failed to add validator",
				zap.Stringer("nodeID", vdrID),
				zap.Stringer("netID", constants.PrimaryNetworkID),
				zap.Error(err),
			)
		}
	}
	// Router.Connected not available in consensus package
	// i.Router.Connected(vdrID, nodeVersion, netID)
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.NodeID) {
	// RemoveWeight will only error here if there was an error reported during
	// Add.
	err := i.vdrs.RemoveWeight(constants.PrimaryNetworkID, vdrID, i.weight)
	if err != nil {
		i.log.Error("failed to remove weight",
			zap.Stringer("nodeID", vdrID),
			zap.Stringer("netID", constants.PrimaryNetworkID),
			zap.Error(err),
		)
	}
	// Router.Disconnected not available in consensus package
	// i.Router.Disconnected(vdrID)
}
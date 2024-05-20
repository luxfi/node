// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync/atomic"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/networking/router"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/timer"
	"github.com/luxfi/node/version"
)

var _ router.Router = (*beaconManager)(nil)

type beaconManager struct {
	router.Router
	timer         *timer.Timer
	beacons       validators.Manager
	requiredConns int64
	numConns      int64
}

func (b *beaconManager) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	_, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID)
	if isBeacon &&
		constants.PrimaryNetworkID == subnetID &&
		atomic.AddInt64(&b.numConns, 1) >= b.requiredConns {
		b.timer.Cancel()
	}
	b.Router.Connected(nodeID, nodeVersion, subnetID)
}

func (b *beaconManager) Disconnected(nodeID ids.NodeID) {
	if _, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID); isBeacon {
		atomic.AddInt64(&b.numConns, -1)
	}
	b.Router.Disconnected(nodeID)
}

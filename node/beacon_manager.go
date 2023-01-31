// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync/atomic"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/networking/router"
	"github.com/luxdefi/node/snow/validators"
	"github.com/luxdefi/node/utils/constants"
	"github.com/luxdefi/node/utils/timer"
	"github.com/luxdefi/node/version"
)

var _ router.Router = (*beaconManager)(nil)

type beaconManager struct {
	router.Router
	timer         *timer.Timer
	beacons       validators.Set
	requiredConns int64
	numConns      int64
}

func (b *beaconManager) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	if constants.PrimaryNetworkID == subnetID &&
		b.beacons.Contains(nodeID) &&
		atomic.AddInt64(&b.numConns, 1) >= b.requiredConns {
		b.timer.Cancel()
	}
	b.Router.Connected(nodeID, nodeVersion, subnetID)
}

func (b *beaconManager) Disconnected(nodeID ids.NodeID) {
	if b.beacons.Contains(nodeID) {
		atomic.AddInt64(&b.numConns, -1)
	}
	b.Router.Disconnected(nodeID)
}

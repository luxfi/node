// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"
	"sync/atomic"

	"github.com/luxfi/ids"
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/constants"
	"github.com/luxfi/node/version"
)

var _ Router = (*beaconManager)(nil)

type beaconManager struct {
	Router
	beacons                     validators.Manager
	requiredConns               int64
	numConns                    int64
	onSufficientlyConnected     chan struct{}
	onceOnSufficientlyConnected sync.Once
}

func (b *beaconManager) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	_, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID)
	if isBeacon &&
		constants.PrimaryNetworkID == netID &&
		atomic.AddInt64(&b.numConns, 1) >= b.requiredConns {
		b.onceOnSufficientlyConnected.Do(func() {
			close(b.onSufficientlyConnected)
		})
	}
	// Router doesn't have Connected method in consensus package
	// b.Router.Connected(nodeID, nodeVersion, netID)
}

func (b *beaconManager) Disconnected(nodeID ids.NodeID) {
	if _, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID); isBeacon {
		atomic.AddInt64(&b.numConns, -1)
	}
	// Router doesn't have Disconnected method in consensus package
	// b.Router.Disconnected(nodeID)
}

// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"

	"github.com/luxfi/node/consensus/networking/router"
	"github.com/luxfi/node/consensus/validators"
)

var _ router.Router = (*beaconManager)(nil)

type beaconManager struct {
	router.Router
	beacons                     validators.Manager
	requiredConns               int64
	numConns                    int64
	onSufficientlyConnected     chan struct{}
	onceOnSufficientlyConnected sync.Once
}

// TODO: The Router interface no longer has Connected/Disconnected methods.
// This functionality needs to be implemented differently, possibly through
// a peer tracker or network handler.

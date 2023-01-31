// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
<<<<<<< HEAD
	"crypto"
	"time"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/message"
	"github.com/luxdefi/luxd/utils/ips"
	"github.com/luxdefi/luxd/version"
=======
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
>>>>>>> 6fc3d3f7c (Remove `Version` from the `peer.Network` interface (#2320))
)

var TestNetwork Network = testNetwork{}

type testNetwork struct{}

func (testNetwork) Connected(ids.NodeID) {}

func (testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (testNetwork) Track(ips.ClaimedIPPort) bool {
	return true
}

func (testNetwork) Disconnected(ids.NodeID) {}

func (testNetwork) Peers(ids.NodeID) ([]ips.ClaimedIPPort, error) {
	return nil, nil
}

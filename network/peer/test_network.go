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
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/ips"
<<<<<<< HEAD
<<<<<<< HEAD
>>>>>>> 6fc3d3f7c (Remove `Version` from the `peer.Network` interface (#2320))
=======

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
>>>>>>> 8fe3833a0 (Support IP updates in PeerList gossip tracking (#2374))
=======
>>>>>>> d4644818b (Add EngineType for ambiguous p2p messages (#2272))
)

var TestNetwork Network = testNetwork{}

type testNetwork struct{}

func (testNetwork) Connected(ids.NodeID) {}

func (testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (testNetwork) Track(ids.NodeID, []*ips.ClaimedIPPort) ([]*p2p.PeerAck, error) {
	return nil, nil
}

func (testNetwork) MarkTracked(ids.NodeID, []*p2p.PeerAck) error {
	return nil
}

func (testNetwork) Disconnected(ids.NodeID) {}

func (testNetwork) Peers(ids.NodeID) ([]ips.ClaimedIPPort, error) {
	return nil, nil
}

// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/proto/pb/p2p"
	"github.com/luxdefi/node/utils/ips"
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

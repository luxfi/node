// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/bloom"
	"github.com/luxfi/net/endpoints"
)

var TestNetwork Network = testNetwork{}

type testNetwork struct{}

func (testNetwork) Connected(ids.NodeID) {}

func (testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (testNetwork) Track([]*endpoints.ClaimedIPPort) error {
	return nil
}

func (testNetwork) Disconnected(ids.NodeID) {}

func (testNetwork) KnownPeers() ([]byte, []byte) {
	return bloom.EmptyFilter.Marshal(), nil
}

func (testNetwork) Peers(
	ids.NodeID,
	set.Set[ids.ID],
	bool,
	*bloom.ReadFilter,
	[]byte,
) []*endpoints.ClaimedIPPort {
	return nil
}

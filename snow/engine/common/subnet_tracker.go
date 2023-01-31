// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/luxdefi/node/ids"
)

// SubnetTracker describes the interface for checking if a node is tracking a
// subnet, namely if a node has whitelisted a subnet.
type SubnetTracker interface {
	// TracksSubnet returns true if [nodeID] tracks [subnetID]
	TracksSubnet(nodeID ids.NodeID, subnetID ids.ID) bool
}

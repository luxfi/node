// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/luxfi/node/ids"
)

// SubnetConnector represents a handler that is called when a connection is
// marked as connected to a subnet
type SubnetConnector interface {
	ConnectedSubnet(ctx context.Context, nodeID ids.NodeID, subnetID ids.ID) error
}

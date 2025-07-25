// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/version"
)

// Connector represents a handler that is called when a connection is marked as
// connected or disconnected
type Connector interface {
	Connected(
		ctx context.Context,
		nodeID ids.NodeID,
		nodeVersion *version.Application,
	) error
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}

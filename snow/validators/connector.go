// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/version"
)

// Connector represents a handler that is called when a connection is marked as
// connected or disconnected
type Connector interface {
	Connected(id ids.NodeID, nodeVersion *version.Application) error
	Disconnected(id ids.NodeID) error
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import "github.com/luxfi/ids"
import "github.com/luxfi/validators"

type Config struct {
	Deprecated bool
}

type Manager interface {
	IsBenched(nodeID ids.NodeID, chainID ids.ID) bool
	GetBenched(chainID ids.ID) []ids.NodeID
	RegisterChain(chainID ids.ID, vdrs validators.Manager) error
	Benchable(chainID ids.ID, nodeID ids.NodeID) Benchable
}

type Benchable interface {
	Benched(chainID ids.ID, nodeID ids.NodeID)
	Unbenched(chainID ids.ID, nodeID ids.NodeID)
}

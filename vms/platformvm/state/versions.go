// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/luxfi/ids"

type Versions interface {
	// GetState returns the state of the chain after [blkID] has been accepted.
	// If the state is not known, `false` will be returned.
	GetState(blkID ids.ID) (Chain, bool)
}

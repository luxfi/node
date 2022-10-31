// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxdefi/luxd/ids"
)

type Versions interface {
	GetState(blkID ids.ID) (Chain, bool)
}

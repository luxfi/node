// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/utils"
)

var _ utils.Sortable[validatorData] = validatorData{}

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

func (d validatorData) Less(other validatorData) bool {
	return d.id.Less(other.id)
}

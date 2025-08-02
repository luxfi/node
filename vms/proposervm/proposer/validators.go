// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils"
)

var _ utils.Sortable[validatorData] = validatorData{}

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

func (d validatorData) Compare(other validatorData) int {
	return d.id.Compare(other.id)
}

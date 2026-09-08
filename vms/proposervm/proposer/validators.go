// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/util"
)

var _ utils.Sortable[validatorData] = validatorData{}

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

func (d validatorData) Compare(other validatorData) int {
	return d.id.Compare(other.id)
}

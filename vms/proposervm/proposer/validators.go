// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var _ utils.Sortable[validatorData] = validatorData{}

type validatorData struct {
	id     ids.NodeID
	weight uint64
}

<<<<<<< HEAD
func (d validatorData) Less(other validatorData) bool {
	return d.id.Less(other.id)
=======
type validatorsSlice []validatorData

func (d validatorsSlice) Len() int {
	return len(d)
}

func (d validatorsSlice) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

func (d validatorsSlice) Less(i, j int) bool {
	iID := d[i].id
	jID := d[j].id
	return bytes.Compare(iID[:], jID[:]) == -1
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
}

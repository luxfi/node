// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ utils.Sortable[*GenesisAsset] = (*GenesisAsset)(nil)

type Genesis struct {
	Txs []*GenesisAsset `serialize:"true"`
}

<<<<<<< HEAD
=======
func (g *Genesis) Less(i, j int) bool {
	return strings.Compare(g.Txs[i].Alias, g.Txs[j].Alias) == -1
}

func (g *Genesis) Len() int {
	return len(g.Txs)
}

func (g *Genesis) Swap(i, j int) {
	g.Txs[j], g.Txs[i] = g.Txs[i], g.Txs[j]
}

func (g *Genesis) Sort() {
	sort.Sort(g)
}

func (g *Genesis) IsSortedAndUnique() bool {
	return utils.IsSortedAndUnique(g)
}

>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
type GenesisAsset struct {
	Alias             string `serialize:"true"`
	txs.CreateAssetTx `serialize:"true"`
}

func (g *GenesisAsset) Less(other *GenesisAsset) bool {
	return g.Alias < other.Alias
}

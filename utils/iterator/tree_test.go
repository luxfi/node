<<<<<<< HEAD:vms/platformvm/state/tree_iterator_test.go
// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
>>>>>>> upstream/master:utils/iterator/tree_test.go
// See the file LICENSE for licensing terms.

package iterator_test

import (
	"testing"
	"time"
	

	"github.com/google/btree"
	"github.com/stretchr/testify/require"

<<<<<<< HEAD:vms/platformvm/state/tree_iterator_test.go
	"github.com/luxfi/ids"
=======
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/iterator"
	"github.com/luxfi/node/vms/platformvm/state"
>>>>>>> upstream/master:utils/iterator/tree_test.go
)

var defaultTreeDegree = 2

func TestTree(t *testing.T) {
	require := require.New(t)
	stakers := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

<<<<<<< HEAD:vms/platformvm/state/tree_iterator_test.go
	tree := btree.NewG(defaultTreeDegree, (*Staker).Less)
=======
	tree := btree.NewG(defaultTreeDegree, (*state.Staker).Less)
>>>>>>> upstream/master:utils/iterator/tree_test.go
	for _, staker := range stakers {
		require.Nil(tree.ReplaceOrInsert(staker))
	}

	it := iterator.FromTree(tree)
	for _, staker := range stakers {
		require.True(it.Next())
		require.Equal(staker, it.Value())
	}
	require.False(it.Next())
	it.Release()
}

<<<<<<< HEAD:vms/platformvm/state/tree_iterator_test.go
func TestTreeIteratorNil(t *testing.T) {
	it := NewTreeIterator(nil)
=======
func TestTreeNil(t *testing.T) {
	it := iterator.FromTree[*state.Staker](nil)
>>>>>>> upstream/master:utils/iterator/tree_test.go
	require.False(t, it.Next())
	it.Release()
}

func TestTreeEarlyRelease(t *testing.T) {
	require := require.New(t)
	stakers := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

<<<<<<< HEAD:vms/platformvm/state/tree_iterator_test.go
	tree := btree.NewG(defaultTreeDegree, (*Staker).Less)
=======
	tree := btree.NewG(defaultTreeDegree, (*state.Staker).Less)
>>>>>>> upstream/master:utils/iterator/tree_test.go
	for _, staker := range stakers {
		require.Nil(tree.ReplaceOrInsert(staker))
	}

	it := iterator.FromTree(tree)
	require.True(it.Next())
	it.Release()
	require.False(it.Next())
}

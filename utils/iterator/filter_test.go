<<<<<<< HEAD:vms/platformvm/state/masked_iterator_test.go
// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
>>>>>>> upstream/master:utils/iterator/filter_test.go
// See the file LICENSE for licensing terms.

package iterator_test

import (
	"testing"
	"time"
	

	"github.com/stretchr/testify/require"

<<<<<<< HEAD:vms/platformvm/state/masked_iterator_test.go
	"github.com/luxfi/ids"
=======
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/platformvm/state"

	. "github.com/luxfi/node/utils/iterator"
>>>>>>> upstream/master:utils/iterator/filter_test.go
)

func TestFilter(t *testing.T) {
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
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}
	maskedStakers := map[ids.ID]*state.Staker{
		stakers[0].TxID: stakers[0],
		stakers[2].TxID: stakers[2],
		stakers[3].TxID: stakers[3],
	}

	it := Filter(
		FromSlice(stakers[:3]...),
		func(staker *state.Staker) bool {
			_, ok := maskedStakers[staker.TxID]
			return ok
		},
	)

	require.True(it.Next())
	require.Equal(stakers[1], it.Value())

	require.False(it.Next())
	it.Release()
	require.False(it.Next())
}

func TestDeduplicate(t *testing.T) {
	require.Equal(
		t,
		[]int{0, 1, 2, 3},
		ToSlice(Deduplicate(FromSlice(0, 1, 2, 1, 2, 0, 3))),
	)
}

// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
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

	it := iterator.Filter(
		iterator.FromSlice(stakers[:3]...),
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

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaintest

import (
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/chain/snowtest"
)

func RequireStatusIs(require *require.Assertions, status snowtest.Status, blks ...*Block) {
	for i, blk := range blks {
		require.Equal(status, blk.Status, i)
	}
}

// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/stretchr/testify/require"

func equalOption(require *require.Assertions, want, have Block) {
	require.Equal(want.ID(), have.ID())
	require.Equal(want.ParentID(), have.ParentID())
	require.Equal(want.Block(), have.Block())
	require.Equal(want.Bytes(), have.Bytes())
}

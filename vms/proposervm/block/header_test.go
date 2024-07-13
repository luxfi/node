// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/stretchr/testify/require"

func equalHeader(require *require.Assertions, want, have Header) {
	require.Equal(want.ChainID(), have.ChainID())
	require.Equal(want.ParentID(), have.ParentID())
	require.Equal(want.BodyID(), have.BodyID())
}

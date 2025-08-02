// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	require := require.New(t)

	h := Hash{}
	require.NoError(h.Set("heytherepal"))
	require.True(h.Check("heytherepal"))
	require.False(h.Check("heytherepal!"))
	require.False(h.Check(""))
}

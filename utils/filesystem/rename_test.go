// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameIfExists(t *testing.T) {
	require := require.New(t)

	t.Parallel()

	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")

	f, err := os.Create(a)
	require.NoError(err)
	require.NoError(f.Close())

	// rename "a" to "b"
	renamed, err := RenameIfExists(a, b)
	require.NoError(err)
	require.True(renamed)

	// rename "b" to "a"
	renamed, err = RenameIfExists(b, a)
	require.NoError(err)
	require.True(renamed)

	// remove "a", but rename "a"->"b" should NOT error
	require.NoError(os.RemoveAll(a))
	renamed, err = RenameIfExists(a, b)
	require.NoError(err)
	require.False(renamed)
}

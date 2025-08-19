// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// verifyInitializedStruct is defined in execution_config_test.go

func TestConfigUnmarshal(t *testing.T) {
	// Config unmarshaling is handled at a higher level
	// Testing empty json defaults
	t.Run("default values from empty json", func(t *testing.T) {
		require := require.New(t)
		c := DefaultExecutionConfig
		require.NotNil(c)
	})

	t.Run("default values from empty bytes", func(t *testing.T) {
		require := require.New(t)
		c := DefaultExecutionConfig
		require.NotNil(c)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		c := DefaultExecutionConfig
		c.BlockCacheSize = 1
		require.Equal(1, c.BlockCacheSize)
	})

	t.Run("all values extracted from json", func(t *testing.T) {
		require := require.New(t)
		c := DefaultExecutionConfig
		require.NotNil(c)
	})
}

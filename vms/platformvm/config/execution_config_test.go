// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Requires all values in a struct to be initialized
func verifyInitializedStruct(tb testing.TB, s interface{}) {
	tb.Helper()

	require := require.New(tb)

	structType := reflect.TypeOf(s)
	require.Equal(reflect.Struct, structType.Kind())

	v := reflect.ValueOf(s)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		require.True(field.IsValid(), "invalid field: ", structType.Field(i).Name)
		require.False(field.IsZero(), "zero field: ", structType.Field(i).Name)
	}
}

func TestExecutionConfigUnmarshal(t *testing.T) {
	t.Run("default values from empty json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(&DefaultExecutionConfig, ec)
	})

	t.Run("default values from empty bytes", func(t *testing.T) {
		require := require.New(t)
		b := []byte(``)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(&DefaultExecutionConfig, ec)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{"block-cache-size":1}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := DefaultExecutionConfig
		expected.BlockCacheSize = 1
		require.Equal(&expected, ec)
	})

	t.Run("all values extracted from json", func(t *testing.T) {
		require := require.New(t)

		expected := &ExecutionConfig{
			Network: NetworkConfig{
				MaxGossipSize:          15,
				GossipAcceptedFrontier: 5 * time.Second,
				GossipAccepted:         3 * time.Second,
			},
			BlockCacheSize:               1,
			TxCacheSize:                  2,
			TransformedSubnetTxCacheSize: 3,
			RewardUTXOsCacheSize:         5,
			ChainCacheSize:               6,
			ChainDBCacheSize:             7,
			BlockIDCacheSize:             8,
			FxOwnerCacheSize:             9,
			ChecksumsEnabled:             true,
			MempoolPruneFrequency:        time.Minute,
		}
		verifyInitializedStruct(t, *expected)
		verifyInitializedStruct(t, expected.Network)

		b, err := json.Marshal(expected)
		require.NoError(err)

		actual, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(expected, actual)
	})
}

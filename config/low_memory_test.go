// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/go-json-experiment/json"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDefaultLowMemoryConfig(t *testing.T) {
	config := DefaultLowMemoryConfig()

	require.False(t, config.Enabled)
	require.Equal(t, uint64(DefaultDBCacheSize), config.DBCacheSize)
	require.Equal(t, uint64(DefaultDBMemtableSize), config.DBMemtableSize)
	require.Equal(t, uint64(DefaultStateCacheSize), config.StateCacheSize)
	require.Equal(t, uint64(DefaultBlockCacheSize), config.BlockCacheSize)
	require.False(t, config.DisableBloomFilters)
	require.False(t, config.LazyChainLoading)
	require.False(t, config.SingleValidatorMode)
}

func TestNewLowMemoryConfig(t *testing.T) {
	config := NewLowMemoryConfig()

	require.True(t, config.Enabled)
	require.Equal(t, uint64(LowMemDBCacheSize), config.DBCacheSize)
	require.Equal(t, uint64(LowMemDBMemtableSize), config.DBMemtableSize)
	require.Equal(t, uint64(LowMemStateCacheSize), config.StateCacheSize)
	require.Equal(t, uint64(LowMemBlockCacheSize), config.BlockCacheSize)
	require.True(t, config.DisableBloomFilters)
	require.True(t, config.LazyChainLoading)
	require.True(t, config.SingleValidatorMode)
}

func TestGetLowMemoryConfig_Default(t *testing.T) {
	v := viper.New()
	config := GetLowMemoryConfig(v)

	require.False(t, config.Enabled)
	require.Equal(t, uint64(DefaultDBCacheSize), config.DBCacheSize)
}

func TestGetLowMemoryConfig_LowMemoryEnabled(t *testing.T) {
	v := viper.New()
	v.Set(LowMemoryKey, true)
	config := GetLowMemoryConfig(v)

	require.True(t, config.Enabled)
	require.Equal(t, uint64(LowMemDBCacheSize), config.DBCacheSize)
	require.True(t, config.LazyChainLoading)
}

func TestGetLowMemoryConfig_DevLightEnabled(t *testing.T) {
	v := viper.New()
	v.Set(DevLightKey, true)
	config := GetLowMemoryConfig(v)

	require.True(t, config.Enabled)
	require.Equal(t, uint64(LowMemDBCacheSize), config.DBCacheSize)
}

func TestGetLowMemoryConfig_ExplicitOverrides(t *testing.T) {
	v := viper.New()
	v.Set(LowMemoryKey, true)
	v.Set(DBCacheSizeKey, uint64(32*MB))
	v.Set(StateCacheSizeKey, uint64(64*MB))

	config := GetLowMemoryConfig(v)

	require.True(t, config.Enabled)
	require.Equal(t, uint64(32*MB), config.DBCacheSize)
	require.Equal(t, uint64(64*MB), config.StateCacheSize)
	// Non-overridden values use low memory defaults
	require.Equal(t, uint64(LowMemDBMemtableSize), config.DBMemtableSize)
}

func TestLoadConfigProfile_DevLight(t *testing.T) {
	profile, err := LoadConfigProfile("dev-light")
	require.NoError(t, err)
	require.NotNil(t, profile)

	// Check expected keys are present
	require.Contains(t, profile, "dev")
	require.Contains(t, profile, "low-memory")
	require.Contains(t, profile, "db-cache-size")

	// Check comment fields are removed
	require.NotContains(t, profile, "_comment")
	require.NotContains(t, profile, "_targets")
}

func TestLoadConfigProfile_NonExistent(t *testing.T) {
	profile, err := LoadConfigProfile("non-existent-profile")
	require.Error(t, err)
	require.Nil(t, profile)
}

func TestLoadConfigProfile_Empty(t *testing.T) {
	profile, err := LoadConfigProfile("")
	require.NoError(t, err)
	require.Nil(t, profile)
}

func TestApplyConfigProfile(t *testing.T) {
	v := viper.New()
	v.Set("existing-key", "existing-value")

	profile := map[string]interface{}{
		"existing-key": "profile-value",
		"new-key":      "profile-new-value",
	}

	ApplyConfigProfile(v, profile)

	// Existing key should not be overwritten
	require.Equal(t, "existing-value", v.GetString("existing-key"))
	// New key should be added
	require.Equal(t, "profile-new-value", v.GetString("new-key"))
}

func TestEstimatedMemoryUsage(t *testing.T) {
	tests := []struct {
		name           string
		config         LowMemoryConfig
		expectedIdle   uint64
		expectedLoad   uint64
		singleValBonus uint64
	}{
		{
			name:   "default config",
			config: DefaultLowMemoryConfig(),
		},
		{
			name:   "low memory config",
			config: NewLowMemoryConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idle, load := tt.config.EstimatedMemoryUsage()

			// Basic sanity checks
			require.Greater(t, idle, uint64(0))
			require.Greater(t, load, uint64(0))
			require.Greater(t, load, idle)

			// Low memory mode should have significantly lower memory usage
			if tt.config.Enabled {
				require.Less(t, idle, uint64(100*MB), "idle memory should be <100MB in low memory mode")
				require.Less(t, load, uint64(200*MB), "load memory should be <200MB in low memory mode")
			}
		})
	}
}

func TestMemorySizeConstants(t *testing.T) {
	// Constants are untyped, so compare as int
	require.Equal(t, 1024, KB)
	require.Equal(t, 1024*1024, MB)
	require.Equal(t, 1024*1024*1024, GB)

	// Verify low < standard < max for all settings
	require.Less(t, LowMemDBCacheSize, DefaultDBCacheSize)
	require.Less(t, LowMemDBMemtableSize, DefaultDBMemtableSize)
	require.Less(t, LowMemStateCacheSize, DefaultStateCacheSize)
	require.Less(t, LowMemBlockCacheSize, DefaultBlockCacheSize)

	require.Less(t, DefaultDBCacheSize, MaxMemDBCacheSize)
	require.Less(t, DefaultDBMemtableSize, MaxMemDBMemtableSize)
	require.Less(t, DefaultStateCacheSize, MaxMemStateCacheSize)
	require.Less(t, DefaultBlockCacheSize, MaxMemBlockCacheSize)
}

func TestMemoryProfile(t *testing.T) {
	// Test low profile
	lowConfig := GetMemoryConfigForProfile(MemoryProfileLow)
	require.Equal(t, uint64(LowMemDBCacheSize), lowConfig.DBCacheSize)
	require.True(t, lowConfig.DisableBloomFilters)

	// Test standard profile
	stdConfig := GetMemoryConfigForProfile(MemoryProfileStandard)
	require.Equal(t, uint64(DefaultDBCacheSize), stdConfig.DBCacheSize)
	require.False(t, stdConfig.DisableBloomFilters)

	// Test max profile
	maxConfig := GetMemoryConfigForProfile(MemoryProfileMax)
	require.Equal(t, uint64(MaxMemDBCacheSize), maxConfig.DBCacheSize)
	require.False(t, maxConfig.DisableBloomFilters)
}

func TestGetLowMemoryConfig_MemoryProfile(t *testing.T) {
	// Test --memory-profile=low
	v := viper.New()
	v.Set(MemoryProfileKey, "low")
	config := GetLowMemoryConfig(v)
	require.True(t, config.Enabled)
	require.Equal(t, uint64(LowMemDBCacheSize), config.DBCacheSize)

	// Test --memory-profile=standard
	v = viper.New()
	v.Set(MemoryProfileKey, "standard")
	config = GetLowMemoryConfig(v)
	require.False(t, config.Enabled)
	require.Equal(t, uint64(DefaultDBCacheSize), config.DBCacheSize)

	// Test --memory-profile=max
	v = viper.New()
	v.Set(MemoryProfileKey, "max")
	config = GetLowMemoryConfig(v)
	require.False(t, config.Enabled)
	require.Equal(t, uint64(MaxMemDBCacheSize), config.DBCacheSize)
}

func TestBuildDatabaseConfigBytes_Disabled(t *testing.T) {
	v := viper.New()
	// Low memory not enabled and no explicit DB settings

	configBytes, err := buildDatabaseConfigBytes(v)
	require.NoError(t, err)
	require.Nil(t, configBytes)
}

func TestBuildDatabaseConfigBytes_LowMemoryEnabled(t *testing.T) {
	v := viper.New()
	v.Set(LowMemoryKey, true)

	configBytes, err := buildDatabaseConfigBytes(v)
	require.NoError(t, err)
	require.NotNil(t, configBytes)

	// Verify JSON can be parsed and has expected structure
	var cfg map[string]interface{}
	err = json.Unmarshal(configBytes, &cfg)
	require.NoError(t, err)

	// Check that reduced memory settings are present
	require.Contains(t, cfg, "memTableSize")
	require.Contains(t, cfg, "blockCacheSize")
	require.Contains(t, cfg, "indexCacheSize")
	require.Contains(t, cfg, "numMemtables")
	require.Contains(t, cfg, "numCompactors")
	require.Contains(t, cfg, "bloomFalsePositive")

	// Verify values are low memory settings
	memTableSize := int64(cfg["memTableSize"].(float64))
	require.Equal(t, int64(LowMemDBMemtableSize), memTableSize)

	blockCacheSize := int64(cfg["blockCacheSize"].(float64))
	require.Equal(t, int64(LowMemDBCacheSize), blockCacheSize)

	numMemtables := int(cfg["numMemtables"].(float64))
	require.Equal(t, 2, numMemtables) // Reduced from default 5
}

func TestBuildDatabaseConfigBytes_BloomFiltersDisabled(t *testing.T) {
	v := viper.New()
	v.Set(LowMemoryKey, true)
	v.Set(DisableBloomFiltersKey, true)

	configBytes, err := buildDatabaseConfigBytes(v)
	require.NoError(t, err)
	require.NotNil(t, configBytes)

	var cfg map[string]interface{}
	err = json.Unmarshal(configBytes, &cfg)
	require.NoError(t, err)

	// Bloom false positive should be 1.0 (effectively disabled)
	bloomFP := cfg["bloomFalsePositive"].(float64)
	require.Equal(t, 1.0, bloomFP)
}

func TestBuildDatabaseConfigBytes_ExplicitOverrides(t *testing.T) {
	v := viper.New()
	v.Set(DBCacheSizeKey, uint64(16*MB))
	v.Set(DBMemtableSizeKey, uint64(16*MB))

	configBytes, err := buildDatabaseConfigBytes(v)
	require.NoError(t, err)
	require.NotNil(t, configBytes)

	var cfg map[string]interface{}
	err = json.Unmarshal(configBytes, &cfg)
	require.NoError(t, err)

	// Verify explicit overrides are used
	blockCacheSize := int64(cfg["blockCacheSize"].(float64))
	require.Equal(t, int64(16*MB), blockCacheSize)

	memTableSize := int64(cfg["memTableSize"].(float64))
	require.Equal(t, int64(16*MB), memTableSize)
}

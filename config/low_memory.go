// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

//go:embed profiles/*.json
var profilesFS embed.FS

// LowMemoryConfig holds memory-related configuration for resource-constrained environments.
// Default mode uses standard production settings.
// Low memory mode targets <200MB idle, <500MB under load.
type LowMemoryConfig struct {
	// Enabled indicates low memory mode is active
	Enabled bool `json:"enabled"`

	// DBCacheSize is the database cache size in bytes
	// Default: 64MB, Low memory: 8MB
	DBCacheSize uint64 `json:"dbCacheSize"`

	// DBMemtableSize is the memtable size in bytes
	// Default: 64MB, Low memory: 8MB
	DBMemtableSize uint64 `json:"dbMemtableSize"`

	// StateCacheSize is the state cache size in bytes
	// Default: 256MB, Low memory: 16MB
	StateCacheSize uint64 `json:"stateCacheSize"`

	// BlockCacheSize is the block cache size in bytes
	// Default: 128MB, Low memory: 8MB
	BlockCacheSize uint64 `json:"blockCacheSize"`

	// DisableBloomFilters disables bloom filters to save memory
	DisableBloomFilters bool `json:"disableBloomFilters"`

	// LazyChainLoading defers chain loading until first request
	LazyChainLoading bool `json:"lazyChainLoading"`

	// SingleValidatorMode runs with a single validator
	SingleValidatorMode bool `json:"singleValidatorMode"`
}

// Memory size constants
const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB

	// Standard settings - target <100MB per node (new default)
	DefaultDBCacheSize    = 16 * MB
	DefaultDBMemtableSize = 16 * MB
	DefaultStateCacheSize = 32 * MB
	DefaultBlockCacheSize = 32 * MB

	// Low memory settings - target <50MB per node
	LowMemDBCacheSize    = 4 * MB
	LowMemDBMemtableSize = 4 * MB
	LowMemStateCacheSize = 8 * MB
	LowMemBlockCacheSize = 4 * MB

	// Max memory settings - production/high-performance
	MaxMemDBCacheSize    = 64 * MB
	MaxMemDBMemtableSize = 64 * MB
	MaxMemStateCacheSize = 256 * MB
	MaxMemBlockCacheSize = 128 * MB
)

// MemoryProfile defines memory usage profiles.
type MemoryProfile string

const (
	// MemoryProfileLow targets <50MB per node - for very resource-constrained environments
	MemoryProfileLow MemoryProfile = "low"
	// MemoryProfileStandard is the default, targets <100MB per node
	MemoryProfileStandard MemoryProfile = "standard"
	// MemoryProfileMax is for production/high-performance, ~512MB per node
	MemoryProfileMax MemoryProfile = "max"
)

// DefaultLowMemoryConfig returns the default configuration (standard profile).
func DefaultLowMemoryConfig() LowMemoryConfig {
	return LowMemoryConfig{
		Enabled:             false,
		DBCacheSize:         DefaultDBCacheSize,
		DBMemtableSize:      DefaultDBMemtableSize,
		StateCacheSize:      DefaultStateCacheSize,
		BlockCacheSize:      DefaultBlockCacheSize,
		DisableBloomFilters: false,
		LazyChainLoading:    false,
		SingleValidatorMode: false,
	}
}

// NewLowMemoryConfig returns a configuration for low memory profile (<50MB).
func NewLowMemoryConfig() LowMemoryConfig {
	return LowMemoryConfig{
		Enabled:             true,
		DBCacheSize:         LowMemDBCacheSize,
		DBMemtableSize:      LowMemDBMemtableSize,
		StateCacheSize:      LowMemStateCacheSize,
		BlockCacheSize:      LowMemBlockCacheSize,
		DisableBloomFilters: true,
		LazyChainLoading:    true,
		SingleValidatorMode: true,
	}
}

// NewMaxMemoryConfig returns a configuration for max memory profile (~512MB).
func NewMaxMemoryConfig() LowMemoryConfig {
	return LowMemoryConfig{
		Enabled:             false,
		DBCacheSize:         MaxMemDBCacheSize,
		DBMemtableSize:      MaxMemDBMemtableSize,
		StateCacheSize:      MaxMemStateCacheSize,
		BlockCacheSize:      MaxMemBlockCacheSize,
		DisableBloomFilters: false,
		LazyChainLoading:    false,
		SingleValidatorMode: false,
	}
}

// GetMemoryConfigForProfile returns config for the given memory profile.
func GetMemoryConfigForProfile(profile MemoryProfile) LowMemoryConfig {
	switch profile {
	case MemoryProfileLow:
		return NewLowMemoryConfig()
	case MemoryProfileMax:
		return NewMaxMemoryConfig()
	default:
		return DefaultLowMemoryConfig()
	}
}

// GetLowMemoryConfig builds a LowMemoryConfig from viper settings.
func GetLowMemoryConfig(v *viper.Viper) LowMemoryConfig {
	// Check for explicit memory profile first
	profileStr := v.GetString(MemoryProfileKey)
	if profileStr != "" {
		profile := MemoryProfile(profileStr)
		config := GetMemoryConfigForProfile(profile)
		config.Enabled = profile == MemoryProfileLow
		return applyOverrides(v, config)
	}

	// Fallback to legacy --low-memory flag
	lowMemEnabled := v.GetBool(LowMemoryKey) || v.GetBool(DevLightKey)

	config := DefaultLowMemoryConfig()
	if lowMemEnabled {
		config = NewLowMemoryConfig()
	}
	return applyOverrides(v, config)
}

// applyOverrides applies explicit flag overrides to the config.
func applyOverrides(v *viper.Viper, config LowMemoryConfig) LowMemoryConfig {
	// Allow explicit flag overrides
	if v.IsSet(DBCacheSizeKey) {
		config.DBCacheSize = v.GetUint64(DBCacheSizeKey)
	}
	if v.IsSet(DBMemtableSizeKey) {
		config.DBMemtableSize = v.GetUint64(DBMemtableSizeKey)
	}
	if v.IsSet(StateCacheSizeKey) {
		config.StateCacheSize = v.GetUint64(StateCacheSizeKey)
	}
	if v.IsSet(BlockCacheSizeKey) {
		config.BlockCacheSize = v.GetUint64(BlockCacheSizeKey)
	}
	if v.IsSet(DisableBloomFiltersKey) {
		config.DisableBloomFilters = v.GetBool(DisableBloomFiltersKey)
	}
	if v.IsSet(LazyChainLoadingKey) {
		config.LazyChainLoading = v.GetBool(LazyChainLoadingKey)
	}
	if v.IsSet(SingleValidatorModeKey) {
		config.SingleValidatorMode = v.GetBool(SingleValidatorModeKey)
	}
	return config
}

// LoadConfigProfile loads a predefined configuration profile from embedded files.
// Returns the profile as a map for merging with viper.
func LoadConfigProfile(name string) (map[string]interface{}, error) {
	if name == "" {
		return nil, nil
	}

	filename := filepath.Join("profiles", name+".json")
	data, err := profilesFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load profile %q: %w", name, err)
	}

	var profile map[string]interface{}
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to parse profile %q: %w", name, err)
	}

	// Remove comment fields
	delete(profile, "_comment")
	delete(profile, "_targets")

	return profile, nil
}

// ApplyConfigProfile applies a configuration profile to viper.
// Profile values are set with lower priority than command-line flags.
func ApplyConfigProfile(v *viper.Viper, profile map[string]interface{}) {
	for key, value := range profile {
		if !v.IsSet(key) {
			v.Set(key, value)
		}
	}
}

// ApplyDevLightMode applies --dev-light settings to viper.
// This is equivalent to --dev --low-memory.
func ApplyDevLightMode(v *viper.Viper) error {
	// Load the dev-light profile
	profile, err := LoadConfigProfile("dev-light")
	if err != nil {
		return err
	}

	// Apply profile settings (won't override explicitly set flags)
	ApplyConfigProfile(v, profile)

	// Ensure dev mode is enabled
	v.Set(DevModeKey, true)
	v.Set(LowMemoryKey, true)

	return nil
}

// EstimatedMemoryUsage returns an estimate of memory usage based on config.
func (c LowMemoryConfig) EstimatedMemoryUsage() (idle, load uint64) {
	// Base memory overhead (runtime, goroutines, etc.)
	baseOverhead := uint64(50 * MB)

	// Idle usage: caches are mostly empty
	idle = baseOverhead + c.DBCacheSize/4 + c.StateCacheSize/4

	// Load usage: caches fill up
	load = baseOverhead + c.DBCacheSize + c.DBMemtableSize + c.StateCacheSize + c.BlockCacheSize

	// Add overhead for network buffers if not in single validator mode
	if !c.SingleValidatorMode {
		load += 50 * MB // Network overhead for multi-validator
	}

	return idle, load
}

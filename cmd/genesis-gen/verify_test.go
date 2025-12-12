// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/stretchr/testify/require"
)

func TestGeneratedGenesisValidation(t *testing.T) {
	// Generate a test genesis
	validators, err := generateValidators(5)
	require.NoError(t, err)

	config, err := createGenesisConfig(12345, validators)
	require.NoError(t, err)

	// Validate it can be unparsed
	unparsedConfig, err := config.Unparse()
	require.NoError(t, err)

	// Validate it can be parsed back
	parsedConfig, err := unparsedConfig.Parse()
	require.NoError(t, err)

	// Validate the parsed config matches
	require.Equal(t, config.NetworkID, parsedConfig.NetworkID)
	require.Equal(t, len(config.Allocations), len(parsedConfig.Allocations))
	require.Equal(t, len(config.InitialStakers), len(parsedConfig.InitialStakers))

	// Validate it can create genesis bytes
	genesisBytes, luxAssetID, err := genesis.FromConfig(&parsedConfig)
	require.NoError(t, err)
	require.NotEmpty(t, genesisBytes)
	require.NotEqual(t, "11111111111111111111111111111111LpoYY", luxAssetID.String())

	t.Logf("Genesis validation successful!")
	t.Logf("Genesis bytes length: %d", len(genesisBytes))
	t.Logf("LUX asset ID: %s", luxAssetID)
}

func TestLoadGenesisFromFile(t *testing.T) {
	// Create temporary file
	tmpFile, err := os.CreateTemp("", "genesis-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Generate validators
	validators, err := generateValidators(3)
	require.NoError(t, err)

	config, err := createGenesisConfig(12345, validators)
	require.NoError(t, err)

	// Write to file
	unparsedConfig, err := config.Unparse()
	require.NoError(t, err)

	// Use genesis package to write and read
	genesisBytes, _, err := genesis.FromConfig(config)
	require.NoError(t, err)

	// Write the unparsed JSON
	require.NoError(t, os.WriteFile(tmpFile.Name(), []byte(mustJSON(unparsedConfig)), 0644))

	// Load it back
	loadedConfig, err := genesis.GetConfigFile(tmpFile.Name())
	require.NoError(t, err)

	require.Equal(t, config.NetworkID, loadedConfig.NetworkID)

	t.Logf("Successfully loaded genesis from file")
	t.Logf("Genesis bytes: %d bytes", len(genesisBytes))
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

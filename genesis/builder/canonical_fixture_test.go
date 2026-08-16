// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"

	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
)

// TestCanonicalGenesisFixtureParses confirms the canonical mainnet genesis.json,
// whose allocation fields are named evmAddr / utxoAddr, parses cleanly via
// GetConfigFile and produces non-zero EVMAddr / UTXOAddr values. A loader that
// rejects the canonical fixture is reading field names nothing writes.
func TestCanonicalGenesisFixtureParses(t *testing.T) {
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "work/lux/genesis/configs/mainnet/genesis.json"),
		"../../../genesis/configs/mainnet/genesis.json",
	}

	var path string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		t.Skip("canonical mainnet genesis fixture not on disk; skipping")
	}

	cfg, err := genesiscfg.GetConfigFile(path)
	require.NoError(t, err, "canonical fixture must parse with v1.12.19 evmAddr/utxoAddr names")
	require.NotEmpty(t, cfg.Allocations, "fixture has no allocations")

	for i, a := range cfg.Allocations {
		require.NotEqual(t, ids.ShortEmpty, a.EVMAddr, "allocation[%d] EVMAddr is zero — parse silently dropped", i)
		require.NotEqual(t, ids.ShortEmpty, a.UTXOAddr, "allocation[%d] UTXOAddr is zero — parse silently dropped", i)
	}
}

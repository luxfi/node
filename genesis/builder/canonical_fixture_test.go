// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"

	genesisconfigs "github.com/luxfi/genesis/configs"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
)

// TestCanonicalGenesisFixtureParses confirms the canonical genesis, whose
// allocation fields are named evmAddr / utxoAddr, parses cleanly through
// GetConfigFile and produces non-zero EVMAddr / UTXOAddr values. A loader that
// rejects the canonical fixture is reading field names nothing writes.
//
// GetConfigFile is the --genesis-file loader, so it is the one path left that
// reads genesis off disk, and the canonical config is the content most worth
// handing it. The fixture is written out from the compiled-in config rather
// than hunted for under $HOME: the old lookup wanted a genesis checkout beside
// the node, found one on a developer's box and nothing in CI, and so asserted
// whatever happened to be on the disk it ran on — usually nothing at all.
func TestCanonicalGenesisFixtureParses(t *testing.T) {
	for _, networkID := range canonicalNetworks {
		t.Run(networkNameOf(networkID), func(t *testing.T) {
			require := require.New(t)

			fixture, err := genesisconfigs.GetGenesisWithAllocations(networkID, nil)
			require.NoError(err)

			path := filepath.Join(t.TempDir(), "genesis.json")
			require.NoError(os.WriteFile(path, fixture, 0o600))

			cfg, err := genesiscfg.GetConfigFile(path)
			require.NoError(err, "the canonical fixture must parse with evmAddr/utxoAddr names")
			require.NotEmpty(cfg.Allocations, "fixture has no allocations")

			for i, a := range cfg.Allocations {
				require.NotEqual(ids.ShortEmpty, a.EVMAddr, "allocation[%d] EVMAddr is zero — parse silently dropped", i)
				require.NotEqual(ids.ShortEmpty, a.UTXOAddr, "allocation[%d] UTXOAddr is zero — parse silently dropped", i)
			}
		})
	}
}

// A node started without --genesis-file joins the network that is running. The
// C-Chain the shipped genesis founds is the one live validators run, named by
// the ID their chainData directory carries.
func TestShippedGenesisFoundsTheLiveCChain(t *testing.T) {
	for _, c := range []struct {
		networkID uint32
		cchain    string
	}{
		{constants.MainnetID, "2Hx3UMuWA6mSQHwZ8SYcqnWUUAj4T3jHbD5HFFa1SiCdPq9TvU"},
		{constants.TestnetID, "5kjSQWfw5GTuyxX5jxwaugpB2DAawsqtCfW8riWpwrFBuWZMi"},
	} {
		t.Run(networkNameOf(c.networkID), func(t *testing.T) {
			require := require.New(t)
			cfg, err := GetConfig(c.networkID)
			require.NoError(err)
			genesisBytes, _, err := FromConfig(cfg)
			require.NoError(err)
			tx, err := VMGenesis(genesisBytes, constants.EVMID)
			require.NoError(err)
			require.Equal(c.cchain, tx.ID().String(), "the shipped genesis founds another C-Chain")
		})
	}
}

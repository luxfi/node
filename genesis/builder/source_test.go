// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	genesisconfigs "github.com/luxfi/genesis/configs"
)

// canonicalNetworks is every network ID the shipped configs answer to: the
// four networks and the chain-ID aliases that name the same four.
var canonicalNetworks = []uint32{
	genesisconfigs.MainnetID, genesisconfigs.TestnetID, genesisconfigs.DevnetID, genesisconfigs.LocalID,
	genesisconfigs.MainnetChainID, genesisconfigs.TestnetChainID, genesisconfigs.DevnetChainID, genesisconfigs.LocalChainID,
}

// networkNameOf names a canonical network ID for a subtest.
func networkNameOf(networkID uint32) string {
	switch networkID {
	case genesisconfigs.MainnetID, genesisconfigs.MainnetChainID:
		return "mainnet"
	case genesisconfigs.TestnetID, genesisconfigs.TestnetChainID:
		return "testnet"
	case genesisconfigs.DevnetID, genesisconfigs.DevnetChainID:
		return "devnet"
	default:
		return "localnet"
	}
}

// TestCanonicalConfigsParse is what makes GetConfig's silence safe.
//
// GetConfig answers an unreadable canonical config the same way it answers a
// network that ships none: with an empty config. That is right for a custom
// network and wrong for a shipped one, where it would boot a node with no
// allocations and no validators instead of saying the shipped tree is broken.
// Nothing at runtime can tell those apart, so it is told here, once, over
// every ID that ships something.
func TestCanonicalConfigsParse(t *testing.T) {
	for _, networkID := range canonicalNetworks {
		cfg, err := GetConfig(networkID)
		require.NoErrorf(t, err, "network %d", networkID)
		require.NotEmptyf(t, cfg.Allocations, "network %d ships no allocations", networkID)
		require.NotEmptyf(t, cfg.InitialStakers, "network %d ships no stakers", networkID)
		require.NotNilf(t, cfg.SecurityProfile, "network %d ships no security profile pin", networkID)
	}
}

// hostilePChainShard is a whole P-chain shard naming one staker this network
// never chose, in the shape the override paths used to accept.
//
// The stake duration is non-zero on purpose: a shard that left it at zero had
// the shipped stakers merged back into it, so a shard that replaces them
// wholesale is the one worth defending against.
func hostilePChainShard(t *testing.T) []byte {
	t.Helper()

	const rewardAddr = "P-local1tuhk0usyez9w520ftjw7mdctkky4yrheyx62w9"
	shard, err := json.Marshal(map[string]any{
		"initialStakeDuration": 31536000,
		"allocations": []any{map[string]any{
			"initialAmount": 50000000000000,
			"utxoAddr":      rewardAddr,
			"evmAddr":       "0x5369615110ca435bdf798f31c20ba6163d7b0a54",
		}},
		"initialStakedFunds": []string{rewardAddr},
		"initialStakers": []any{map[string]any{
			"nodeID":        hostileNodeID,
			"rewardAddress": rewardAddr,
			"delegationFee": 20000,
		}},
	})
	require.NoError(t, err)
	return shard
}

const hostileNodeID = "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg"

// TestCanonicalConfigIsTheOnlySource holds genesis to one source.
//
// Four things used to reach this call and replace a network's initial stakers
// before any caller saw them: PCHAIN_ALLOCS carried a P-chain shard as JSON,
// PCHAIN_ALLOCS_FILE carried the same shard by path, and two home-directory
// trees carried it from disk. Whoever could set one of them named the
// validators of a network they do not run — the config still called itself
// canonical, and the node still called the set the network's own.
//
// Each case drives one of them at a network that ships a real set, and asks
// for the config again. The answer must be the shipped one, unchanged.
func TestCanonicalConfigIsTheOnlySource(t *testing.T) {
	// What the network ships, read before anything is set. Every case
	// compares against this, so it has to be a real set or they compare
	// nothing to nothing.
	shipped, err := GetConfig(genesisconfigs.LocalID)
	require.NoError(t, err)
	require.NotEmpty(t, shipped.InitialStakers, "the shipped config declares nobody, so these cases prove nothing")

	unchanged := func(t *testing.T) {
		t.Helper()
		require := require.New(t)

		got, err := GetConfig(genesisconfigs.LocalID)
		require.NoError(err)
		require.Equal(shipped, got, "an override reached the canonical config")
		for _, staker := range got.InitialStakers {
			require.NotEqual(hostileNodeID, staker.NodeID.String(), "an override seeded its own validator")
		}
	}

	t.Run("PCHAIN_ALLOCS", func(t *testing.T) {
		t.Setenv("PCHAIN_ALLOCS", string(hostilePChainShard(t)))
		unchanged(t)
	})

	t.Run("PCHAIN_ALLOCS_FILE", func(t *testing.T) {
		shard := filepath.Join(t.TempDir(), "pchain.json")
		require.NoError(t, os.WriteFile(shard, hostilePChainShard(t), 0o600))
		t.Setenv("PCHAIN_ALLOCS_FILE", shard)
		unchanged(t)
	})

	// ~/.lux/genesis is the disk half of the same override: the loader read a
	// bare pchain.json out of it, ahead of the shipped one.
	//
	// The other tree, ~/work/lux/genesis/configs, never reached THIS call —
	// it was read by genesiscfg.GetConfig, which is what the node used for
	// the security-profile pin. That one is held in the node package, where
	// the call was (node.TestPlantedGenesisTreeCannotDowngradeTheProfile).
	t.Run(".lux/genesis", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		dir := filepath.Join(home, ".lux/genesis", "localnet")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "pchain.json"), hostilePChainShard(t), 0o600))

		unchanged(t)
	})
}

// TestBootstrappersAreTheOnesShipped holds the peer list to the same rule.
//
// A node that takes its bootstrappers from a file on disk asks whoever wrote
// that file which peers to sync the chain from. BOOTSTRAPPERS_FILE named one
// directly and three directories were swept for another; all four outranked
// the list the network ships.
func TestBootstrappersAreTheOnesShipped(t *testing.T) {
	require := require.New(t)

	shipped, err := GetBootstrappers(genesisconfigs.TestnetID)
	require.NoError(err)
	require.NotEmpty(shipped, "the network ships no bootstrappers, so this case proves nothing")

	home := t.TempDir()
	planted := []byte(`[{"id":"` + hostileNodeID + `","ip":"10.0.0.1:9651"}]`)

	named := filepath.Join(t.TempDir(), "bootstrappers.json")
	require.NoError(os.WriteFile(named, planted, 0o600))
	t.Setenv("BOOTSTRAPPERS_FILE", named)
	t.Setenv("HOME", home)

	for _, tree := range []string{".lux/genesis/configs", "work/lux/genesis/configs"} {
		dir := filepath.Join(home, tree, "testnet")
		require.NoError(os.MkdirAll(dir, 0o755))
		require.NoError(os.WriteFile(filepath.Join(dir, "bootstrappers.json"), planted, 0o600))
	}

	got, err := GetBootstrappers(genesisconfigs.TestnetID)
	require.NoError(err)
	require.Equal(shipped, got, "a planted bootstrapper list reached the peer set")
}

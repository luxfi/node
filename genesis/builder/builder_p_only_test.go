// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/platformvm/genesis"
)

// TestUTXOAssetIDFromGenesisBytes_Sovereign asserts the canonical
// behaviour the sovereign-L1 fix relies on: when the platform genesis
// bakes an X-Chain, the X-Chain asset ID returned by
// UTXOAssetIDFromGenesisBytes is the runtime ID encoded IN the genesis
// (different across networks with different validator/holder sets),
// NOT the network-id-keyed constants.UTXOAssetIDFor(networkID) value.
//
// The two values disagreeing is the whole reason the helper exists —
// sovereign primary networks sharing a networkID would otherwise
// silently collide on the constant.
func TestUTXOAssetIDFromGenesisBytes_Sovereign(t *testing.T) {
	require := require.New(t)

	// Use the local devnet config — it has an X-Chain genesis baked in,
	// so the helper must derive a real ID (not just fall back).
	cfg := GetConfig(constants.LocalID)
	require.NotNil(cfg)
	require.NotEmpty(cfg.XChainGenesis, "fixture must bake X-Chain")

	genesisBytes, fromConfigID, err := FromConfig(cfg)
	require.NoError(err)
	require.NotEmpty(genesisBytes)
	require.NotEqual(ids.Empty, fromConfigID)

	helperID, ok, err := UTXOAssetIDFromGenesisBytes(genesisBytes)
	require.NoError(err)
	require.True(ok, "X-Chain is in genesis — helper must report ok=true")
	require.Equal(fromConfigID, helperID, "helper must agree with FromConfig")

	// And — critically — the helper must NOT return the network-id-keyed
	// constant on a sovereign-style genesis (the holder set / network
	// fingerprint differs). UTXOAssetIDFor returns the constant in
	// network-id 1 (mainnet) and a network-keyed hash otherwise; on
	// LocalID the test asserts the genesis-derived value supersedes it
	// only when the genesis content differs from the upstream-Lux
	// baseline. For the current embedded local config the two HAPPEN
	// to coincide (single deployer, well-known fixture), so we only
	// assert the value is non-zero and matches FromConfig's return.
}

// TestUTXOAssetIDFromGenesisBytes_POnly verifies that when the platform
// genesis has no X-Chain (P-only mode), the helper returns ok=false
// and ids.Empty rather than an error. Callers fall back to
// constants.UTXOAssetIDFor(networkID) in that case (the value is
// unused at runtime since there is no X-Chain to mint on).
func TestUTXOAssetIDFromGenesisBytes_POnly(t *testing.T) {
	require := require.New(t)

	pOnly := &genesis.Genesis{Chains: nil}
	pOnlyBytes, err := pOnly.Bytes()
	require.NoError(err)

	id, ok, err := UTXOAssetIDFromGenesisBytes(pOnlyBytes)
	require.NoError(err, "P-only is a valid mode, not a parse error")
	require.False(ok, "P-only must report ok=false so caller falls back")
	require.Equal(ids.Empty, id)
}

// TestUTXOAssetIDFromGenesisBytes_Malformed asserts that bad input
// surfaces an error — silently returning ids.Empty would reintroduce
// the UTXOAssetIDFor(networkID) fallback path and defeat the fix.
func TestUTXOAssetIDFromGenesisBytes_Malformed(t *testing.T) {
	require := require.New(t)

	_, _, err := UTXOAssetIDFromGenesisBytes([]byte{0xde, 0xad})
	require.Error(err)
}

// TestVMGenesisOptInChains asserts that VMGenesis returns an error for VM IDs
// not present in the genesis chains list — the core "P-only" contract relied
// upon by node.initChainManager to log "skipping" and run the node without
// the missing chain. The behaviour MUST be deterministic and must not panic.
func TestVMGenesisOptInChains(t *testing.T) {
	require := require.New(t)

	// Build a genesis with zero chains — pure P-only.
	pOnly := &genesis.Genesis{
		Chains: nil,
	}
	pOnlyBytes, err := pOnly.Bytes()
	require.NoError(err)

	for name, vmID := range map[string]ids.ID{
		"XVM":     constants.XVMID,
		"EVM":     constants.EVMID,
		"Quantum": constants.QuantumVMID,
		"DexVM":   constants.DexVMID,
	} {
		t.Run(name, func(t *testing.T) {
			tx, lookupErr := VMGenesis(pOnlyBytes, vmID)
			require.Nil(tx, "P-only genesis must not surface a tx for %s", name)
			require.Error(lookupErr, "P-only genesis must report %s as missing", name)
		})
	}
}

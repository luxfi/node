// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"
)

// TestChainAliasesRegistryParity asserts that the registry-derived
// {P,X,C,...}ChainAliases vars hold the same slices as the legacy
// hand-typed literals. If this test ever fails, the registry data
// drifted from the legacy chain alias contract — either rebrand on
// purpose (and update legacy literals here) or fix the Registry row.
func TestChainAliasesRegistryParity(t *testing.T) {
	tests := []struct {
		name   string
		got    []string
		legacy []string
	}{
		{"P", PChainAliases, []string{"P", "platform"}},
		{"X", XChainAliases, []string{"X", "xvm"}},
		{"C", CChainAliases, []string{"C", "evm"}},
		{"D", DChainAliases, []string{"D", "dex", "dexvm"}},
		{"Q", QChainAliases, []string{"Q", "quantum", "quantumvm", "pq"}},
		{"A", AChainAliases, []string{"A", "attest", "ai", "aivm"}},
		{"B", BChainAliases, []string{"B", "bridge", "bridgevm"}},
		{"M", MChainAliases, []string{"M", "mpc", "mpcvm"}},
		{"F", FChainAliases, []string{"F", "fhe", "fhevm"}},
		{"Z", ZChainAliases, []string{"Z", "zk", "zkvm"}},
		{"G", GChainAliases, []string{"G", "graph", "graphvm", "dgraph"}},
		{"K", KChainAliases, []string{"K", "key", "keyvm"}},
		{"I", IChainAliases, []string{"I", "identity", "identityvm", "id"}},
		{"O", OChainAliases, []string{"O", "oracle", "oraclevm", "feed"}},
		{"R", RChainAliases, []string{"R", "relay", "relayvm", "msg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t,
				reflect.DeepEqual(tt.got, tt.legacy),
				"%sChainAliases drift: got %v, legacy %v", tt.name, tt.got, tt.legacy,
			)
		})
	}
}

// TestVMAliasesRegistryParity asserts that VMAliases holds the same
// (VMID -> alias set) the legacy literal map encoded. Alias order is
// not required (the alias manager treats each entry as a name
// mapping), so this test compares sorted alias sets.
func TestVMAliasesRegistryParity(t *testing.T) {
	legacy := map[ids.ID][]string{
		constants.PlatformVMID:  {"platform"},
		constants.XVMID:         {"xvm"},
		constants.EVMID:         {"evm"},
		constants.DexVMID:       {"dexvm", "dex"},
		constants.QuantumVMID:   {"quantumvm", "quantum", "pq"},
		constants.AIVMID:        {"aivm", "attest", "ai"},
		constants.BridgeVMID:    {"bridgevm", "bridge"},
		constants.MPCVMID:       {"mpc", "mpcvm"},
		constants.FHEVMID:       {"fhe", "fhevm"},
		constants.ZKVMID:        {"zkvm", "zk"},
		constants.GraphVMID:     {"graphvm", "graph", "dgraph"},
		constants.KeyVMID:       {"keyvm", "key"},
		constants.IdentityVMID:  {"identity", "identityvm", "id"},
		constants.OracleVMID:    {"oracle", "oraclevm", "feed"},
		constants.RelayVMID:     {"relay", "relayvm", "msg"},
		secp256k1fx.ID:          {"secp256k1fx"},
		nftfx.ID:                {"nftfx"},
		propertyfx.ID:           {"propertyfx"},
		mldsafx.ID:              {"mldsafx"},
		slhdsafx.ID:             {"slhdsafx"},
		ed25519fx.ID:            {"ed25519fx"},
		secp256r1fx.ID:          {"secp256r1fx"},
		schnorrfx.ID:            {"schnorrfx"},
		bls12381fx.ID:           {"bls12381fx"},
	}

	require.Equal(t, len(legacy), len(VMAliases), "VMAliases entry count drift")

	for vmID, want := range legacy {
		got, ok := VMAliases[vmID]
		require.True(t, ok, "VMAliases missing entry for VMID %s", vmID)

		wantSorted := append([]string{}, want...)
		gotSorted := append([]string{}, got...)
		sort.Strings(wantSorted)
		sort.Strings(gotSorted)
		require.Equal(t, wantSorted, gotSorted, "VMAliases[%s] alias-set drift: got %v, want %v", vmID, got, want)
	}
}

// TestVMAliasesMapIsolation asserts that mutating the returned map's
// slices does not corrupt the underlying Registry. Each call must
// return fresh copies.
func TestVMAliasesMapIsolation(t *testing.T) {
	a := VMAliasesMap()
	b := VMAliasesMap()

	// Mutate a's slice for the X-Chain.
	a[constants.XVMID][0] = "MUTATED"

	require.NotEqual(t, "MUTATED", b[constants.XVMID][0],
		"VMAliasesMap returns shared slices — mutation leaked across calls")
	require.NotEqual(t, "MUTATED", XChainAliases[1],
		"VMAliasesMap returns slices aliased with package vars")
}

// TestAliasesForUnknown returns nil for letters that have no row in
// Registry — used so callers don't accidentally register a phantom
// chain via a typo.
func TestAliasesForUnknown(t *testing.T) {
	require.Nil(t, AliasesFor(""))
	require.Nil(t, AliasesFor("?"))
	require.Nil(t, AliasesFor("XX"))
}

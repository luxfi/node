// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
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

// ChainSpec is one row in the primary-network chain registry.
//
// Registry is the single source of truth for: canonical letter,
// VMID, bech32 chain prefix used in addresses, alias list, and the
// human-readable name. Rebranding or renaming a chain edits one
// row; the prior switch-ladders in Aliases() and var blocks of
// {P,X,C,...}ChainAliases re-derive from this table.
type ChainSpec struct {
	// Letter is the canonical single-character chain code ("P", "X", ...).
	// Used as the first element of the chain alias list and as the
	// bech32 chain prefix.
	Letter string

	// VMID is the VM that runs this chain. The (VMID -> aliases) map
	// VMAliasesMap() is derived from Registry by collecting Aliases
	// for each ChainSpec.
	VMID ids.ID

	// Aliases are the non-letter aliases for this chain. The full
	// chain alias list returned by AliasesFor is [Letter] ++ Aliases.
	// These are also the VM aliases registered with the alias manager.
	Aliases []string

	// Name is the human-readable chain name ("P-Chain", "C-Chain", ...).
	// Embedded into the primary-network CreateChainTx.
	Name string
}

// Registry is the canonical list of primary-network chains.
//
// Order is fixed and matches the optIn order in FromConfig — reordering
// shifts the P-Chain genesis byte layout for chains that share a
// presence/absence shape, so this slice is append-only.
//
// P-Chain is implicit (the platform genesis itself); listed here so the
// alias machinery has a row for it, but FromConfig does not iterate this
// slice to emit CreateChainTx for P.
var Registry = []ChainSpec{
	{Letter: "P", VMID: constants.PlatformVMID, Aliases: []string{"platform"}, Name: "P-Chain"},
	{Letter: "X", VMID: constants.XVMID, Aliases: []string{"xvm"}, Name: "X-Chain"},
	{Letter: "C", VMID: constants.EVMID, Aliases: []string{"evm"}, Name: "C-Chain"},
	{Letter: "D", VMID: constants.DexVMID, Aliases: []string{"dex", "dexvm"}, Name: "D-Chain"},
	{Letter: "Q", VMID: constants.QuantumVMID, Aliases: []string{"quantum", "quantumvm", "pq"}, Name: "Q-Chain"},
	{Letter: "A", VMID: constants.AIVMID, Aliases: []string{"attest", "ai", "aivm"}, Name: "A-Chain"},
	{Letter: "B", VMID: constants.BridgeVMID, Aliases: []string{"bridge", "bridgevm"}, Name: "B-Chain"},
	{Letter: "T", VMID: constants.ThresholdVMID, Aliases: []string{"threshold", "thresholdvm", "mpc"}, Name: "T-Chain"},
	{Letter: "Z", VMID: constants.ZKVMID, Aliases: []string{"zk", "zkvm"}, Name: "Z-Chain"},
	{Letter: "G", VMID: constants.GraphVMID, Aliases: []string{"graph", "graphvm", "dgraph"}, Name: "G-Chain"},
	{Letter: "K", VMID: constants.KeyVMID, Aliases: []string{"key", "keyvm"}, Name: "K-Chain"},
	{Letter: "I", VMID: constants.IdentityVMID, Aliases: []string{"identity", "identityvm", "id"}, Name: "I-Chain"},
	{Letter: "O", VMID: constants.OracleVMID, Aliases: []string{"oracle", "oraclevm", "feed"}, Name: "O-Chain"},
	{Letter: "R", VMID: constants.RelayVMID, Aliases: []string{"relay", "relayvm", "msg"}, Name: "R-Chain"},
}

// specsByLetter is an O(1) lookup built once at package init.
var specsByLetter = func() map[string]ChainSpec {
	m := make(map[string]ChainSpec, len(Registry))
	for _, s := range Registry {
		m[s.Letter] = s
	}
	return m
}()

// AliasesFor returns the full chain alias list for a chain by letter.
// The letter itself is prepended, e.g. AliasesFor("D") -> ["D", "dex", "dexvm"].
// Returns nil for unknown letters.
func AliasesFor(letter string) []string {
	s, ok := specsByLetter[letter]
	if !ok {
		return nil
	}
	out := make([]string, 0, 1+len(s.Aliases))
	out = append(out, s.Letter)
	out = append(out, s.Aliases...)
	return out
}

// fxVMAliases is the static (non-chain) VM alias table for feature-extension
// VMIDs. These are independent of the chain registry — they are not chains,
// they are credential/output type extensions registered with the VM manager.
var fxVMAliases = map[ids.ID][]string{
	secp256k1fx.ID: {"secp256k1fx"},
	nftfx.ID:       {"nftfx"},
	propertyfx.ID:  {"propertyfx"},
	mldsafx.ID:     {"mldsafx"},
	slhdsafx.ID:    {"slhdsafx"},
	ed25519fx.ID:   {"ed25519fx"},
	secp256r1fx.ID: {"secp256r1fx"},
	schnorrfx.ID:   {"schnorrfx"},
	bls12381fx.ID:  {"bls12381fx"},
}

// VMAliasesMap returns the union of chain VM aliases (from Registry) and
// the static fx VM aliases (from fxVMAliases). This is the map the node's
// VM manager consumes via builder.VMAliases.
//
// The values are fresh slice copies so callers cannot mutate Registry rows
// through the returned map.
func VMAliasesMap() map[ids.ID][]string {
	m := make(map[ids.ID][]string, len(Registry)+len(fxVMAliases))
	for _, s := range Registry {
		aliases := make([]string, len(s.Aliases))
		copy(aliases, s.Aliases)
		m[s.VMID] = aliases
	}
	for vmID, aliases := range fxVMAliases {
		cp := make([]string, len(aliases))
		copy(cp, aliases)
		m[vmID] = cp
	}
	return m
}


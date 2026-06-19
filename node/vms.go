// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"fmt"

	"github.com/luxfi/chains/quantumvm"
	quantumvmconfig "github.com/luxfi/chains/quantumvm/config"
	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/vms"
)

// The node's VM model has exactly two registries, and they are the single
// source of truth for where every VM lives. A VM is in EXACTLY ONE of them —
// that disjointness is what makes static (in-process) registration unable to
// shadow a PluginDir plugin.
//
//   - CoreVMs:     in-process, no NFT gate, available at genesis/core boot.
//                  {P platformvm, X xvm, Q quantumvm, Z zkvm}.
//   - OptionalVMs: plugin-only (loaded from PluginDir via the upstream
//                  VMRegistry scan), NFT-gated activation when RequiredNFT is
//                  set. {D dexvm, B bridgevm, C evm, A/G/I/K/O/R/T, future app
//                  VMs}.
//
// The accessor rule that makes shadowing impossible: a VMID is resolved as
// core (in-process factory) IFF it is in CoreVMs; otherwise it is resolved
// from OptionalVMs and loaded from PluginDir. Because the two maps are
// disjoint (assertRegistriesDisjoint, enforced at init and in tests) and no
// OptionalVMs id is ever handed to VMManager.RegisterFactory
// (assertNoOptionalShadows, enforced at boot before the PluginDir scan), the
// upstream registry's "skip any VMID the manager already has"
// (vms/registry/registry.go) can never fire for an optional VM — its plugin
// always wins because the in-process registry never contains it.

// CoreVM describes a VM that runs in-process (linked into luxd), never loaded
// from PluginDir and never NFT-gated.
//
// Factory is the concrete in-process factory for VMs whose construction needs
// no node runtime state (Q, Z) — registerCoreVMs installs these. It is nil for
// P and X, whose factories must be built with live node dependencies
// (chainManager, validators, security profile, classical-compat registry) and
// are therefore registered explicitly in node.go; RegisteredInNodeGo records
// that ownership so the nil Factory is self-documenting, not a footgun.
type CoreVM struct {
	Name               string
	Factory            vms.Factory
	RegisteredInNodeGo bool
}

// NFTRequirement is the per-network-independent POLICY half of a VM's
// activation gate: which X-Chain nftfx collection a validator's staking
// address must hold, and which group within it (0 = any group). The per-network
// VALUE half — the concrete collection AssetID — is supplied separately from
// network config (Config.NFTAuthorizationAssets) and joined with this policy in
// chainAuthorizationsFor. Splitting policy (here) from per-network value
// (config) keeps one declaration of "which VMs need an operator NFT" while the
// asset IDs vary per network/genesis.
type NFTRequirement struct {
	// Collection is the human label of the operator NFT collection (e.g.
	// "dex-operator", "bridge-operator"). It exists for logging/clarity and to
	// key the per-network asset lookup conceptually; the authoritative join key
	// is the VMID (see chainAuthorizationsFor).
	Collection string
	// GroupID is the nftfx group within the collection that authorizes
	// activation; 0 means any group in the collection.
	GroupID uint32
}

// PluginSpec describes a VM that is loaded ONLY from PluginDir (never linked
// in-process), optionally gated on an X-Chain operator NFT.
type PluginSpec struct {
	// Name is the chain VM's package/plugin name (e.g. "dexvm"). For the nine
	// luxfi/chains VMs the plugin main is github.com/luxfi/chains/<Name>/cmd/
	// plugin and the built artifact is placed at <plugin-dir>/<VMID-CB58> by the
	// Dockerfile Chain VM Plugin Stage. The C-Chain EVM ("evm") is also
	// plugin-loaded but its plugin main lives in the EVM repo, not chains.
	Name string
	// RequiredNFT, when non-nil, gates this VM's activation on X-Chain operator
	// NFT ownership (see NFTRequirement). Nil means the VM is plugin-loaded but
	// ungated (any node that tracks the chain may activate it).
	RequiredNFT *NFTRequirement
}

// CoreVMs is the authoritative set of in-process VMs. Q and Z carry their
// concrete factory (registerCoreVMs installs them); P and X are registered in
// node.go with runtime dependencies (RegisteredInNodeGo=true, Factory nil).
//
// This map is the single source for "what is core". The anti-shadow guards
// iterate its keys; tests assert all four are present and that none also
// appears in OptionalVMs.
var CoreVMs = map[ids.ID]CoreVM{
	constants.PlatformVMID: {Name: "platformvm (P-Chain)", RegisteredInNodeGo: true},
	constants.XVMID:        {Name: "xvm (X-Chain)", RegisteredInNodeGo: true},
	constants.QuantumVMID:  {Name: "quantumvm (Q-Chain)", Factory: &quantumvm.Factory{Config: quantumvmconfig.DefaultConfig()}},
	constants.ZKVMID:       {Name: "zkvm (Z-Chain)", Factory: &zkvm.Factory{}},
}

// OptionalVMs is the authoritative set of plugin-only VMs and their activation
// gates. It is the single source for both (a) which VMs MUST NOT be linked
// in-process, and (b) which chains require an operator NFT to activate.
//
// D (dexvm) and B (bridgevm) are NFT-gated: a node may only track/validate
// them if its staking X-address holds the corresponding operator NFT. The
// concrete collection AssetID is per-network and supplied via
// Config.NFTAuthorizationAssets; minting the real collections is a follow-up,
// so until a network configures an asset the chain stays ungated (preserving
// today's opt-in-flag behavior) while the gate itself remains fail-closed.
//
// C (evm) and the remaining app VMs are plugin-loaded but ungated today.
var OptionalVMs = map[ids.ID]PluginSpec{
	constants.DexVMID:       {Name: "dexvm", RequiredNFT: &NFTRequirement{Collection: "dex-operator", GroupID: 0}},
	constants.BridgeVMID:    {Name: "bridgevm", RequiredNFT: &NFTRequirement{Collection: "bridge-operator", GroupID: 0}},
	constants.EVMID:         {Name: "evm"},
	constants.AIVMID:        {Name: "aivm"},
	constants.GraphVMID:     {Name: "graphvm"},
	constants.IdentityVMID:  {Name: "identityvm"},
	constants.KeyVMID:       {Name: "keyvm"},
	constants.OracleVMID:    {Name: "oraclevm"},
	constants.RelayVMID:     {Name: "relayvm"},
	constants.ThresholdVMID: {Name: "thresholdvm"},
}

func init() {
	// Static structural invariant: the two registries must be disjoint. A VM
	// listed as both core and optional is a programming error knowable without
	// any runtime state, so we fail at package init (process never starts).
	if err := assertRegistriesDisjoint(); err != nil {
		panic(err)
	}
}

// assertRegistriesDisjoint reports an error if any VMID appears in both
// CoreVMs and OptionalVMs. It reads only the two static maps, so it is a pure
// check usable from init and from tests. This is the first anti-shadow layer:
// it guarantees no VM is simultaneously declared in-process and plugin-only.
func assertRegistriesDisjoint() error {
	for id := range OptionalVMs {
		if core, ok := CoreVMs[id]; ok {
			return fmt.Errorf("VM %s declared in BOTH CoreVMs (%s) and OptionalVMs (%s): "+
				"an optional/plugin VM must never also be a core/in-process VM, or static "+
				"registration would shadow its PluginDir plugin", id, core.Name, OptionalVMs[id].Name)
		}
	}
	return nil
}

// registerCoreVMs registers the in-process core VMs that carry a concrete
// factory (Q, Z). P and X are registered separately in node.go because their
// factories require live node dependencies (RegisteredInNodeGo=true in
// CoreVMs). The optional chain VMs (A/B/C/D/G/I/K/O/R/T) are NEVER registered
// here — they load from PluginDir via the VMRegistry scan, exactly like any
// other plugin. Registering an optional VM in-process would shadow its plugin
// (the registry skips any VMID the manager already has — vms/registry/
// registry.go) and force luxd to link it, defeating the all-plugins directive.
func (n *Node) registerCoreVMs() error {
	registered := 0
	for id, core := range CoreVMs {
		if core.Factory == nil {
			// P/X: constructed with runtime deps in node.go.
			continue
		}
		if err := n.VMManager.RegisterFactory(context.Background(), id, core.Factory); err != nil {
			n.Log.Warn("Failed to register core VM", "name", core.Name, "error", err)
			continue
		}
		n.Log.Info("Core VM registered in-process", "name", core.Name)
		registered++
	}
	n.Log.Info("Core VMs registered in-process (from CoreVMs registry)", "count", registered)
	n.Log.Info("Optional chain VMs load from PluginDir (OptionalVMs registry)", "count", len(OptionalVMs))
	return nil
}

// assertNoOptionalShadows is the runtime anti-shadow guard. It enumerates the
// in-process VM registry (after ALL in-process registration: P/X in node.go +
// CoreVMs here) and fails if any OptionalVMs id is present. node.go calls this
// BEFORE the PluginDir scan (VMRegistry.Reload), so a violation aborts boot
// before any plugin could be shadowed.
//
// This complements assertRegistriesDisjoint: the static check proves the two
// declarations are disjoint; this runtime check proves the LIVE manager never
// acquired an optional VM by any path (a stray RegisterFactory elsewhere, a
// future regression). Together they make optional-VM shadowing structurally
// impossible.
func (n *Node) assertNoOptionalShadows(ctx context.Context) error {
	inProcess, err := n.VMManager.ListFactories(ctx)
	if err != nil {
		return fmt.Errorf("anti-shadow guard: list in-process VM factories: %w", err)
	}
	registered := make(map[ids.ID]struct{}, len(inProcess))
	for _, id := range inProcess {
		registered[id] = struct{}{}
	}
	for id, spec := range OptionalVMs {
		if _, shadowed := registered[id]; shadowed {
			return fmt.Errorf("anti-shadow violation: optional VM %s (%s) is registered "+
				"in-process; it MUST load only from PluginDir or its plugin will be shadowed "+
				"(the VMRegistry skips any VMID already in the manager)", id, spec.Name)
		}
	}
	n.Log.Info("anti-shadow guard passed: no optional VM is registered in-process",
		"inProcessCount", len(inProcess),
		"optionalCount", len(OptionalVMs),
	)
	return nil
}

// chainAuthorizationsFor builds the manager's ChainAuthorizations map — the
// single mapping of "which chain requires which operator NFT" — from
// OptionalVMs (the policy) joined with the per-network asset IDs (the values)
// and genesis (the chain IDs). It is the ONE place this mapping is derived, so
// OptionalVMs[id].RequiredNFT is the sole declaration of NFT-gating; node.go
// merely passes the result to chains.ManagerConfig.
//
// For each OptionalVMs entry with a RequiredNFT:
//   - if the network has not configured a collection AssetID for that VMID,
//     the chain is left UNGATED (no map entry) — collections are minted as a
//     follow-up, so until then the chain behaves as today (opt-in flag only);
//   - if the chain is not present in this network's genesis, it is skipped;
//   - otherwise the genesis-derived chain ID maps to NFTAuthorization{asset,
//     group}, so authorizeChainActivation fails closed for any node whose
//     staking X-address does not hold the NFT.
func chainAuthorizationsFor(genesisBytes []byte, assets map[ids.ID]ids.ID) map[ids.ID]chains.NFTAuthorization {
	out := make(map[ids.ID]chains.NFTAuthorization)
	for vmID, spec := range OptionalVMs {
		if spec.RequiredNFT == nil {
			continue
		}
		asset, ok := assets[vmID]
		if !ok || asset == ids.Empty {
			continue
		}
		createTx, err := builder.VMGenesis(genesisBytes, vmID)
		if err != nil {
			continue
		}
		out[createTx.ID()] = chains.NFTAuthorization{
			AssetID: asset,
			GroupID: spec.RequiredNFT.GroupID,
		}
	}
	return out
}

// participatesInDEXChain reports this node's D-Chain DEX opt-in — whether the
// operator asked to track/validate the D-Chain and dial the venue matcher
// engine. It is a thin read of Config.DexValidator used for the node's startup
// log line; the SAME bool is passed to ManagerConfig.DexValidator, where the
// chain manager actually ENFORCES it (authorizeChainActivation declines the
// D-Chain when the flag is off). So there is one source of truth (the config
// flag) with one enforcement point (the gate); this accessor is sugar for the
// log, not a parallel policy.
//
// Default is false: most validators never run the DEX. The licensed/private
// piece is the GPU venue ENGINE (lx/dex cmd/dvenue, cgo) — never the node.
//
// The effective activation rule for the D-Chain is flag AND NFT: the chain
// manager's authorizeChainActivation requires DexValidator=true AND, when a
// network has configured the dex-operator collection (OptionalVMs[DexVMID].
// RequiredNFT joined with Config.NFTAuthorizationAssets), the node's staking
// X-address to hold that NFT. With the flag off the D-Chain is declined even
// under --track-all-chains; with the flag on but the NFT unheld the gate fails
// closed. (Phase-2 seam: wiring the node's staking X-address into
// ManagerConfig.StakingXAddress and minting the dex-operator collection per
// network; until a collection is configured the NFT half is a no-op and the
// flag alone governs.)
func (n *Node) participatesInDEXChain() bool {
	return n.Config.DexValidator
}

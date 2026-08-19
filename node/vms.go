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
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/vms"
)

// The node's VM model has exactly two registries, and they are the single
// source of truth for where every VM lives. A VM is in EXACTLY ONE of them —
// that disjointness is what makes static (in-process) registration unable to
// shadow a PluginDir plugin.
//
//   - CoreVMs:     in-process, never restricted, available at genesis/core boot.
//                  {P platformvm, X xvm, Q quantumvm, Z zkvm}.
//   - OptionalVMs: plugin-only (loaded from PluginDir via the upstream
//                  VMRegistry scan), activation restricted to entitled nodes
//                  when Restricted is set. {D dexvm, B bridgevm, C evm,
//                  A/G/I/K/O/R/T, future app VMs}.
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
// from PluginDir and never restricted.
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

// PluginSpec describes a VM that is loaded ONLY from PluginDir (never linked
// in-process), and whether its chain is restricted to entitled nodes.
type PluginSpec struct {
	// Name is the chain VM's package/plugin name (e.g. "dexvm"). For the nine
	// luxfi/chains VMs the plugin main is github.com/luxfi/chains/<Name>/cmd/
	// plugin and the built artifact is placed at <plugin-dir>/<VMID-CB58> by the
	// Dockerfile Chain VM Plugin Stage. The C-Chain EVM ("evm") is also
	// plugin-loaded but its plugin main lives in the EVM repo, not chains.
	Name string
	// Restricted means a node may activate this VM's chain only when the M-Chain
	// holds an ownership attestation naming it. False means plugin-loaded but open
	// to any node that tracks the chain.
	//
	// WHICH collection confers the entitlement is deliberately not stated here. It
	// is asserted once, by whoever reads that collection and asks the M-Chain to
	// sign; a second copy of that pin in the node would be a second source of
	// truth, able to drift from the first.
	Restricted bool
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
// rules. It is the single source for both (a) which VMs MUST NOT be linked
// in-process, and (b) which chains are restricted to entitled nodes.
//
// D (dexvm) is restricted: a node may track/validate it only if the M-Chain holds
// an ownership attestation naming it. That attestation is what makes the
// credential unforgeable by the node's own operator — it lives in consensus
// state, not in local config. The DEX also carries an operator opt-in
// (dex-validator), so an entitlement composes with a service a node chooses to
// offer.
//
// B (bridgevm) is NOT restricted, and the reason is the security argument rather
// than a relaxation of it. B is a primary-network chain, and a primary-network
// validator validates every primary-network chain; the set that secures the
// bridge is already the set that secures P, X and C. Admitting only the subset of
// that set holding a particular token would run the bridge — the surface that
// custodies value crossing between networks — on FEWER nodes than the consensus
// underwriting it, which is the concentration the entitlement exists to prevent,
// pointed the wrong way. A chain every validator must validate cannot be gated on
// a credential only some validators hold.
//
// C (evm) and the remaining app VMs are plugin-loaded but open.
//
// Declaring a VM here is a statement of intent, not a guarantee that a binary
// exists: the registry scan simply finds nothing and the chain cannot start.
// fhevm (F-Chain) is exactly that case today — see its entry below.
var OptionalVMs = map[ids.ID]PluginSpec{
	constants.DexVMID:      {Name: "dexvm", Restricted: true},
	constants.BridgeVMID:   {Name: "bridgevm"},
	constants.EVMID:        {Name: "evm"},
	constants.AIVMID:       {Name: "aivm"},
	constants.GraphVMID:    {Name: "graphvm"},
	constants.IdentityVMID: {Name: "identityvm"},
	constants.KeyVMID:      {Name: "keyvm"},
	constants.OracleVMID:   {Name: "oraclevm"},
	constants.RelayVMID:    {Name: "relayvm"},
	constants.MPCVMID:      {Name: "mpcvm"},
	// F-Chain. Reserved ID, no shipping VM: luxfi/chains has no fhevm/
	// directory, so `make` produces no fhevm binary and this scan always comes
	// up empty. The FHE runtime library lives at luxfi/chains/mpcvm/fhe as a
	// package inside mpcvm, not as a standalone chain. F-Chain is a spec
	// (LP-8200, LP-167). Kept declared so the intent stays visible and the ID
	// stays reserved — remove it only when F-Chain is cancelled, not merely
	// because it is unbuilt.
	constants.FHEVMID: {Name: "fhevm"},
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
// CoreVMs). The optional chain VMs (A/B/C/D/F/G/I/K/M/O/R — the keys of
// OptionalVMs; there is no T, LP-134 dissolved it) are NEVER registered
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

// restrictedChainsFor resolves OptionalVMs' Restricted flags into the set of
// chain IDs the manager enforces. It is the ONE place that mapping is derived, so
// OptionalVMs is the sole declaration of which chains are restricted; node.go
// merely passes the result to chains.ManagerConfig.
//
// A restricted VM absent from this network's genesis is skipped — there is no
// chain to restrict. Everything else in the set refuses to activate unless the
// M-Chain holds an ownership attestation for this node, so the set can only ever
// withhold activation, never grant it.
func restrictedChainsFor(genesisBytes []byte) set.Set[ids.ID] {
	out := set.NewSet[ids.ID](len(OptionalVMs))
	for vmID, spec := range OptionalVMs {
		if !spec.Restricted {
			continue
		}
		createTx, err := builder.VMGenesis(genesisBytes, vmID)
		if err != nil {
			continue
		}
		out.Add(createTx.ID())
	}
	return out
}

// participatesInDEXChain reports this node's D-Chain DEX opt-in — whether the
// operator asked to track/validate the D-Chain and dial the venue matcher
// engine. It is a thin read of Config.DexValidator used for the node's startup
// log line; the SAME bool is passed to ManagerConfig.DexValidator, where the
// chain manager actually ENFORCES it (authorizeChainActivation declines the
// D-Chain when the flag is off). So there is one source of truth (the config
// flag) with one enforcement point (authorizeChainActivation); this accessor is
// sugar for the log, not a parallel policy.
//
// Default is false: most validators never run the DEX. The licensed/private
// piece is the GPU venue ENGINE (lx/dex cmd/dvenue, cgo) — never the node.
//
// The effective activation rule for the D-Chain is opt-in AND entitlement:
// authorizeChainActivation requires DexValidator=true AND an M-Chain ownership
// attestation naming this node. With the flag off the D-Chain is declined even
// under --track-all-chains; with the flag on but no attestation it refuses. The
// flag alone is never sufficient — it states what the operator WANTS, while the
// attestation states what they are entitled to, and only the latter lives
// somewhere the operator cannot rewrite.
func (n *Node) participatesInDEXChain() bool {
	return n.Config.DexValidator
}

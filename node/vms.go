// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"fmt"

	"github.com/luxfi/chains/aivm"
	"github.com/luxfi/chains/bridgevm"
	"github.com/luxfi/chains/fhevm"
	"github.com/luxfi/chains/graphvm"
	"github.com/luxfi/chains/identityvm"
	"github.com/luxfi/chains/keyvm"
	"github.com/luxfi/chains/mpcvm"
	"github.com/luxfi/chains/oraclevm"
	"github.com/luxfi/chains/quantumvm"
	quantumvmconfig "github.com/luxfi/chains/quantumvm/config"
	"github.com/luxfi/chains/relayvm"
	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/constants"
	"github.com/luxfi/evm/plugin/evm"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/vms"
)

// VMs is the node's one VM registry: every chain this daemon can run, and the
// factory that builds it. Registration is in-process — luxd links its chains
// the way it links P and X, and a chain in genesis starts because the binary
// already contains it.
//
// The alternative, one subprocess per chain speaking ZAP over a pipe, buys
// nothing here. The VMs are Go packages in the same module graph; running them
// out-of-process adds a serialization boundary, a supervision problem and a
// failure mode where a chain silently never starts because a file was not
// installed under the right name. It also made "which chains do I have" a
// question about a directory listing rather than about the program.
type VM struct {
	Name string
	// Factory builds the VM. Nil means this entry is not built here: either
	// InNodeGo (P and X, whose factories need live node state) or Plugin (D,
	// which brings its own binary).
	Factory vms.Factory
	// InNodeGo records that node.go owns this registration, so a nil Factory
	// is self-documenting rather than a hole.
	InNodeGo bool
	// Plugin means the VM is served by its own binary out of PluginDir, and a
	// nil Factory here is the point rather than an omission.
	Plugin bool
	// Consent means an operator must name this chain before the node runs it.
	// It is not a security control — anyone may set the flag — it records that
	// running the chain is a decision rather than a default, because the chain
	// costs something a validator did not sign up for by validating the
	// primary network: co-location with a matcher, an HSM, a GPU, a custody
	// role. A chain becomes permissionless by dropping this, and nothing else
	// changes.
	Consent bool

	// Restricted means the chain activates only when the M-Chain holds an
	// ownership attestation naming this node. Nothing sets it today: the
	// verifier and the claim encoding exist (luxfi/chains/ownership) but there
	// is nowhere to read an attestation from, and a check that can only refuse
	// is a refusal wearing a policy's name.
	//
	// Consent and Restricted answer different questions and compose AND-wise:
	// consent is what the operator WANTS, the attestation is what they are
	// ENTITLED to, and only the second lives somewhere the operator cannot
	// rewrite.
	Restricted bool
}

var VMs = map[ids.ID]VM{
	// P and X are built in node.go: their factories take the chain manager,
	// the validator set and the security profile, none of which exist yet at
	// package scope.
	constants.PlatformVMID: {Name: "platformvm (P-Chain)", InNodeGo: true},
	constants.XVMID:        {Name: "xvm (X-Chain)", InNodeGo: true},

	constants.QuantumVMID: {Name: "quantumvm (Q-Chain)", Factory: &quantumvm.Factory{Config: quantumvmconfig.DefaultConfig()}},
	constants.ZKVMID:      {Name: "zkvm (Z-Chain)", Factory: &zkvm.Factory{}},
	constants.EVMID:       {Name: "evm (C-Chain)", Factory: &evm.Factory{}},
	constants.BridgeVMID:  {Name: "bridgevm (B-Chain)", Factory: &bridgevm.Factory{}, Consent: true},
	constants.AIVMID:      {Name: "aivm (A-Chain)", Factory: &aivm.Factory{}},
	constants.GraphVMID:   {Name: "graphvm (G-Chain)", Factory: &graphvm.Factory{}},

	constants.IdentityVMID: {Name: "identityvm (I-Chain)", Factory: &identityvm.Factory{}},
	constants.KeyVMID:      {Name: "keyvm (K-Chain)", Factory: &keyvm.Factory{}},
	constants.OracleVMID:   {Name: "oraclevm (O-Chain)", Factory: &oraclevm.Factory{}},
	constants.RelayVMID:    {Name: "relayvm (R-Chain)", Factory: &relayvm.Factory{}},
	constants.MPCVMID:      {Name: "mpcvm (M-Chain)", Factory: &mpcvm.Factory{}, Consent: true},
	constants.FHEVMID:      {Name: "fhevm (F-Chain)", Factory: &fhevm.Factory{}},

	// D is the one VM luxd does NOT link, and the reason is the matcher. The
	// DEX has two of them — a pure-Go one and a GPU one behind a build tag —
	// and linking the VM would make luxd's build decide which the DEX runs.
	// That is the DEX's decision, so it belongs to the DEX's binary: dexd is
	// built with or without the GPU matcher and installed at
	// <plugin-dir>/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr, and luxd
	// reaches it over ZAP on the same machine.
	//
	// The C-Chain's V4 PoolManager precompile at 0x9999 forwards its swap path
	// to that chain over the same local hop.
	constants.DexVMID: {Name: "dexvm (D-Chain)", Plugin: true, Consent: true},
}

// registerVMs installs every factory in VMs. P and X are registered by node.go
// beforehand; D has nothing to register.
func (n *Node) registerVMs() error {
	registered := 0
	for id, vm := range VMs {
		if vm.Factory == nil {
			if vm.Plugin {
				n.Log.Info("VM loads from PluginDir", "name", vm.Name, "dir", n.Config.PluginDir)
			}
			continue
		}
		if err := n.VMManager.RegisterFactory(context.Background(), id, vm.Factory); err != nil {
			n.Log.Warn("Failed to register VM", "name", vm.Name, "error", err)
			continue
		}
		n.Log.Info("VM registered", "name", vm.Name)
		registered++
	}
	n.Log.Info("VMs registered in-process", "count", registered, "declared", len(VMs))
	return nil
}

// consentChainsFor resolves the Consent flags into the chain IDs that require
// the operator to name them. Same shape as restrictedChainsFor, and for the
// same reason: VMs is the sole declaration, and a chain absent from this
// network's genesis is skipped because there is nothing to consent to.
func consentChainsFor(genesisBytes []byte) set.Set[ids.ID] {
	return chainsWhere(genesisBytes, func(vm VM) bool { return vm.Consent })
}

// consentNames maps each consent-requiring chain ID to the VM's name, so the
// startup lines and the decline can say WHICH chain. An operator reading
// "chainID=JRa11Xfu..." cannot act on it; the whole point of the flag is that
// they choose by name.
func consentNames(genesisBytes []byte) map[ids.ID]string {
	out := make(map[ids.ID]string, len(VMs))
	for vmID, vm := range VMs {
		if !vm.Consent {
			continue
		}
		createTx, err := builder.VMGenesis(genesisBytes, vmID)
		if err != nil {
			continue
		}
		out[createTx.ID()] = vm.Name
	}
	return out
}

// restrictedChainsFor resolves the Restricted flags into the chain IDs the
// manager enforces. VMs is the sole declaration; node.go passes the result to
// chains.ManagerConfig. A restricted VM absent from this network's genesis is
// skipped — there is no chain to restrict — so the set can only ever withhold
// activation, never grant it.
func restrictedChainsFor(genesisBytes []byte) set.Set[ids.ID] {
	return chainsWhere(genesisBytes, func(vm VM) bool { return vm.Restricted })
}

// chainsWhere resolves a predicate over VMs into the genesis chain IDs it
// selects. Both activation sets are built from it, so "which VMs" and "which
// chain IDs" are answered in one place instead of once per policy.
func chainsWhere(genesisBytes []byte, pick func(VM) bool) set.Set[ids.ID] {
	out := set.NewSet[ids.ID](len(VMs))
	for vmID, vm := range VMs {
		if !pick(vm) {
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

// consentedChains resolves the operator's --chains list into genesis chain IDs.
// A name no chain answers to, or one absent from this network's genesis, is an
// error rather than a silent omission: an operator who misspells a chain has
// asked for something, and starting without it looks exactly like the chain
// being broken.
func consentedChains(genesisBytes []byte, names []string) (set.Set[ids.ID], error) {
	out := set.NewSet[ids.ID](len(names))
	for _, name := range names {
		vmID, ok := builder.VMByName(name)
		if !ok {
			return nil, fmt.Errorf("--%s: no chain is named %q", config.ChainsKey, name)
		}
		createTx, err := builder.VMGenesis(genesisBytes, vmID)
		if err != nil {
			return nil, fmt.Errorf("--%s: chain %q is not in this network's genesis", config.ChainsKey, name)
		}
		out.Add(createTx.ID())
	}
	return out, nil
}

// consents reports whether the operator named this chain in --chains. It is a
// thin read used for the startup log lines; the SAME set goes to
// ManagerConfig.Consented, where authorizeChainActivation enforces it. One
// source of truth, one enforcement point; this accessor is sugar for the log.
func (n *Node) consents(chainID ids.ID) bool {
	return n.consented.Contains(chainID)
}

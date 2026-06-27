// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"os/exec"
	"strings"
	"testing"
)

// goListDeps returns the transitive package dependency list of pkg as reported
// by `go list -deps`, run from the module root (two levels up from this
// package's node/node directory). It is the ground-truth dep graph of the
// compiled artifact — the only honest way to assert what a binary links.
func goListDeps(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = ".." // module root: .../node (this test lives in .../node/node)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s failed: %v\n%s", pkg, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func countContaining(deps []string, needle string) int {
	n := 0
	for _, d := range deps {
		if strings.Contains(d, needle) {
			n++
		}
	}
	return n
}

// TestOptionalVMsNotLinkedInProcess enforces the directive "all VMs as plugins
// 100% to keep luxd small" for the nine optional / NFT-gated chain VMs.
//
// These VMs build as CGO=0 plugins (Dockerfile Chain VM Plugin Stage, placed at
// <plugin-dir>/<VMID>) and load through VMRegistry.Reload's PluginDir scan,
// exactly like the C-Chain EVM. They MUST NOT be compiled into the node: an
// in-process RegisterFactory would shadow the plugin (the registry skips any
// VMID the manager already has — vms/registry/registry.go) and force luxd to
// link every VM, defeating the directive.
//
// The four core VMs are deliberately NOT asserted here: P(platformvm) and
// X(xvm) are foundational primary-network VMs, and Q(quantumvm)/Z(zkvm) stay
// in-process for now (Q is the post-quantum chain and is critical by default).
// Turning Q/Z into plugins is a separate design-first phase.
func TestOptionalVMsNotLinkedInProcess(t *testing.T) {
	optionalVMs := []string{
		"github.com/luxfi/chains/aivm",
		"github.com/luxfi/chains/bridgevm",
		"github.com/luxfi/chains/dexvm",
		"github.com/luxfi/chains/graphvm",
		"github.com/luxfi/chains/identityvm",
		"github.com/luxfi/chains/keyvm",
		"github.com/luxfi/chains/oraclevm",
		"github.com/luxfi/chains/relayvm",
		"github.com/luxfi/chains/thresholdvm",
	}
	for _, pkg := range []string{"./node/", "./main"} {
		deps := goListDeps(t, pkg)
		for _, vm := range optionalVMs {
			if got := countContaining(deps, vm); got != 0 {
				t.Errorf("%s links %q (%d packages); the optional chain VMs must "+
					"load from PluginDir, not be compiled in-process (directive: "+
					"all VMs as plugins to keep luxd small)", pkg, vm, got)
			}
		}
	}
}

// TestVenueEngineNotLinked guards the REAL boundary that the (now deleted)
// purity gate was reaching for: the node links the pure-Go D-Chain PROXY, but
// MUST NOT link the private/licensed venue ENGINE or its GPU deps. Those live
// in the separate venue engine (lx/dex cmd/dvenue + luxcpp + gpu-kernels), not
// in the node. The node dials the venue over ZAP at runtime; it never compiles
// the matcher/GPU kernels in.
func TestVenueEngineNotLinked(t *testing.T) {
	forbidden := []string{
		"luxcpp",      // C++ GPU bindings (private)
		"gpu-kernels", // CUDA/Metal/HIP matcher kernels (private)
		"lux-private", // the private workspace
		"luxfi/dex",   // the matcher/venue wrapper (CGO/GPU engine)
	}
	for _, pkg := range []string{"./node/", "./main"} {
		deps := goListDeps(t, pkg)
		for _, f := range forbidden {
			if got := countContaining(deps, f); got != 0 {
				t.Errorf("%s links %q (%d packages); the private venue engine "+
					"and its GPU deps must NOT be in the node", pkg, f, got)
			}
		}
	}
}

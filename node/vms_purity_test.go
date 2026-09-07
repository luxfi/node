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

// TestVenueEngineNotLinked guards the real boundary: the node may link the
// public settlement helpers in luxfi/dex (the C-Chain EVM verifies DEX settle
// imports with them), but it MUST NOT link the licensed venue ENGINE or its GPU
// bindings. Those live in lx/dex cmd/dvenue, luxcpp and gpu-kernels. The node
// dials the venue over ZAP at runtime; it never compiles the matcher in.
//
// Naming the whole luxfi/dex module here would be wrong twice over: the module
// is public, and its GPU matcher is behind a dexgpu build tag this binary never
// sets, so a module-prefix ban refuses a package that carries no engine.
func TestVenueEngineNotLinked(t *testing.T) {
	forbidden := []string{
		"luxcpp",                  // C++ GPU bindings (private)
		"gpu-kernels",             // CUDA/Metal/HIP matcher kernels (private)
		"lux-private",             // the private workspace
		"luxfi/dex/pkg/lxgpu",     // the GPU matcher bindings
		"luxfi/dex/pkg/venue",     // the venue engine
		"luxfi/dex/pkg/orderbook", // the CGO order book
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

// TestNoChainIsEntitlementGated asserts nothing is entitlement-gated. The
// verifier and the claim encoding exist, but no VM answers Entitlement(node)
// where the gate looks for it, so a gate set here would refuse on every node
// forever. Restore one only once the M-Chain can answer the question.
func TestNoChainIsEntitlementGated(t *testing.T) {
	for id, vm := range VMs {
		if vm.Restricted {
			t.Errorf("%s (%s) is entitlement-gated, but nothing can satisfy the gate", id, vm.Name)
		}
	}
}

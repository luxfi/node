// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"os"
	"strings"
	"testing"

	"github.com/luxfi/ids"
)

// dSlot is the canonical D-Chain VM id, derived rather than pasted: a chain's
// VMID is its name in ASCII, zero-padded to 32 bytes. luxd installs a VM by
// writing its binary to <PluginDir>/<dSlot> and executing it, so this id alone
// decides which binary drives the D-Chain.
var dSlot = func() ids.ID {
	var id ids.ID
	copy(id[:], "dexvm")
	return id
}()

// dockerfile is the image build, one level up from this package.
const dockerfile = "../Dockerfile"

// slotBuilds returns the package each `go build -o <PluginDir>/<dSlot>` compiles
// into the D-Chain slot, one entry per build, plus the RUN block each came from.
// Lines that merely reference the installed file (chmod, test -s, COPY) are not
// builds and are not counted: they act on whatever a build already put there.
func slotBuilds(t *testing.T) (targets []string, blocks []string) {
	t.Helper()
	raw, err := os.ReadFile(dockerfile)
	if err != nil {
		t.Fatalf("reading the image build: %v", err)
	}
	slot := dSlot.String()

	// Group lines into RUN blocks so a build can be read together with the
	// assertions guarding it. Continuations belong to the block they extend.
	var block []string
	var all [][]string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "RUN ") && len(block) > 0 {
			all = append(all, block)
			block = nil
		}
		block = append(block, line)
	}
	all = append(all, block)

	for _, b := range all {
		joined := strings.Join(b, "\n")
		for _, line := range b {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f != "-o" || i+2 >= len(fields) {
					continue
				}
				if !strings.HasSuffix(fields[i+1], slot) {
					continue
				}
				targets = append(targets, fields[i+2])
				blocks = append(blocks, joined)
			}
		}
	}
	return targets, blocks
}

// TestCanonicalDSlotHasOneArtifact asserts exactly one build writes the D-Chain
// slot, and that it builds the venue's own command.
//
// The D VM is luxfi/dex cmd/dexd: the matcher that runs the order book inside
// consensus. A second build writing the same path does not fail the image — it
// wins or loses by ordering, and the losing outcome is a node that loads a VM,
// reports healthy, and serves a chain nobody can reach.
func TestCanonicalDSlotHasOneArtifact(t *testing.T) {
	targets, _ := slotBuilds(t)
	if len(targets) != 1 {
		t.Fatalf("%d builds write the D-Chain slot %s (%v); a slot holds one binary, "+
			"and a second build for it is decided by whichever runs last",
			len(targets), dSlot, targets)
	}
	if targets[0] != "./cmd/dexd" {
		t.Errorf("the D-Chain slot %s is built from %q, want ./cmd/dexd — the venue "+
			"command that runs the matcher inside consensus", dSlot, targets[0])
	}
}

// TestCanonicalDSlotSpeaksDexNamespace asserts the build refuses a D-Chain source
// whose wire is not the one clients dial.
//
// A VM is reachable only through the method names it answers. The venue registers
// dex_* (pkg/zapwire); clob_* was the wire of a proxy that has been deleted. A
// plugin serving names nothing calls still loads and still passes health, so this
// has to be caught where the source is present — at build, against the pinned
// tree — rather than by a runtime probe that reports the chain up.
func TestCanonicalDSlotSpeaksDexNamespace(t *testing.T) {
	_, blocks := slotBuilds(t)
	if len(blocks) == 0 {
		t.Fatal("no build writes the D-Chain slot; the Dockerfile parse is broken, not the invariant")
	}
	for _, b := range blocks {
		if !strings.Contains(b, "dex_place") {
			t.Errorf("the D-Chain build does not assert the venue registers the dex_* wire; "+
				"a source whose methods no client dials would build and install clean\n"+
				"slot: %s", dSlot)
		}
		if !strings.Contains(b, "clob_") {
			t.Errorf("the D-Chain build does not refuse the dead clob_* wire; the deleted "+
				"proxy's method names must not reappear in the slot's source\nslot: %s", dSlot)
		}
	}
}

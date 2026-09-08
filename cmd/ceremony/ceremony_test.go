package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCeremonyInitContributeVerify(t *testing.T) {
	dir := t.TempDir()

	// Init
	state, err := initCeremony("quasar", 1<<14, 3)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}
	if state.PowersNeeded != (1<<14)+1 {
		t.Fatalf("expected %d powers, got %d", (1<<14)+1, state.PowersNeeded)
	}

	// Write initial state
	initPath := filepath.Join(dir, "c0.json")
	if err := writeState(state, initPath); err != nil {
		t.Fatalf("writeState init: %v", err)
	}

	// Three contributions
	participants := []string{"Alice", "Bob", "Carol"}
	prevPath := initPath
	for i, p := range participants {
		st, err := readState(prevPath)
		if err != nil {
			t.Fatalf("readState round %d: %v", i, err)
		}

		contrib, err := contribute(st, p)
		if err != nil {
			t.Fatalf("contribute %s: %v", p, err)
		}
		st.Contributions = append(st.Contributions, contrib)

		outPath := filepath.Join(dir, "c"+string(rune('1'+i))+".json")
		if err := writeState(st, outPath); err != nil {
			t.Fatalf("writeState %s: %v", p, err)
		}
		prevPath = outPath
	}

	// Verify final state
	finalState, err := readState(prevPath)
	if err != nil {
		t.Fatalf("readState final: %v", err)
	}

	if len(finalState.Contributions) != 3 {
		t.Fatalf("expected 3 contributions, got %d", len(finalState.Contributions))
	}

	if err := verifyCeremony(finalState); err != nil {
		t.Fatalf("verifyCeremony: %v", err)
	}

	// Verify hash chain integrity
	for i, c := range finalState.Contributions {
		if c.StateHash == "" {
			t.Fatalf("contribution %d has empty StateHash", i)
		}
		if i == 0 && c.PrevHash != "" {
			t.Fatalf("first contribution should have empty PrevHash")
		}
		if i > 0 && c.PrevHash != finalState.Contributions[i-1].StateHash {
			t.Fatalf("contribution %d PrevHash mismatch", i)
		}
	}
}

func TestCeremonyIntegrityTamper(t *testing.T) {
	dir := t.TempDir()

	state, err := initCeremony("test", 1<<4, 1)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	contrib, err := contribute(state, "Mallory")
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	state.Contributions = append(state.Contributions, contrib)

	path := filepath.Join(dir, "tamper.json")
	if err := writeState(state, path); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	// Tamper with the file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Flip a byte in the middle
	mid := len(data) / 2
	data[mid] ^= 0xff
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	// Should fail integrity check
	_, err = readState(path)
	if err == nil {
		t.Fatal("expected integrity check failure on tampered file")
	}
}

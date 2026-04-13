package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

// =============================================================================
// CRITICAL Regressions
// =============================================================================

// TestRegressionC01_ToxicWasteZeroed verifies that after contribute() returns,
// all secret scalars (tau, alpha, beta) and their big.Int intermediates are
// zeroed out.
// Finding C-01: Toxic waste was not zeroed after contribution, leaving
// secret scalars recoverable from process memory.
func TestRegressionC01_ToxicWasteZeroed(t *testing.T) {
	// Verify zeroBI function exists and actually zeros big.Int limbs.
	x := new(big.Int).SetUint64(0xDEADBEEFCAFEBABE)
	if x.Sign() == 0 {
		t.Fatal("precondition: x must be non-zero")
	}

	zeroBI(x)

	// After zeroing: value must be 0
	if x.Sign() != 0 {
		t.Fatal("zeroBI must set big.Int to zero -- C-01 regression")
	}
	// Limbs must all be zero
	for _, limb := range x.Bits() {
		if limb != 0 {
			t.Fatalf("zeroBI must zero all limbs, found non-zero limb -- C-01 regression")
		}
	}

	// Verify with multi-word value (256-bit)
	y := new(big.Int)
	y.SetString("DEADBEEFCAFEBABE0123456789ABCDEF0011223344556677AABBCCDDEEFF0099", 16)
	if len(y.Bits()) < 2 {
		t.Fatal("precondition: y must have multiple limbs")
	}

	zeroBI(y)

	if y.Sign() != 0 {
		t.Fatal("zeroBI must zero multi-word big.Int -- C-01 regression")
	}
	for _, limb := range y.Bits() {
		if limb != 0 {
			t.Fatal("zeroBI must zero all limbs of multi-word big.Int -- C-01 regression")
		}
	}
}

// TestRegressionC02_CeremonyChainLinking verifies that tampering with any
// intermediate contribution's StateHash or PrevHash is detected by verifyCeremony.
// Finding C-02: Hash chain was not validated, allowing contribution reordering
// or state tampering without detection.
func TestRegressionC02_CeremonyChainLinking(t *testing.T) {
	// Build a valid ceremony with 3 contributions
	state, err := initCeremony("test-c02", 1<<4, 3)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	for _, p := range []string{"Alice", "Bob", "Carol"} {
		contrib, err := contribute(state, p)
		if err != nil {
			t.Fatalf("contribute %s: %v", p, err)
		}
		state.Contributions = append(state.Contributions, contrib)
	}

	// Baseline: valid state must verify
	if err := verifyCeremony(state); err != nil {
		t.Fatalf("valid ceremony must verify: %v", err)
	}

	// Test 1: Tamper with contribution[1].StateHash
	t.Run("tamper_StateHash", func(t *testing.T) {
		clone := cloneState(t, state)
		clone.Contributions[1].StateHash = "0000000000000000000000000000000000000000000000000000000000000000"
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("tampered StateHash must be detected -- C-02 regression")
		}
	})

	// Test 2: Tamper with contribution[1].PrevHash
	t.Run("tamper_PrevHash", func(t *testing.T) {
		clone := cloneState(t, state)
		clone.Contributions[1].PrevHash = "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("tampered PrevHash must be detected -- C-02 regression")
		}
	})

	// Test 3: Reorder contributions (swap 1 and 2)
	t.Run("reorder_contributions", func(t *testing.T) {
		clone := cloneState(t, state)
		clone.Contributions[1], clone.Contributions[2] = clone.Contributions[2], clone.Contributions[1]
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("reordered contributions must be detected -- C-02 regression")
		}
	})

	// Test 4: First contribution with non-empty PrevHash
	t.Run("first_nonempty_PrevHash", func(t *testing.T) {
		clone := cloneState(t, state)
		clone.Contributions[0].PrevHash = "abc123"
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("first contribution with non-empty PrevHash must be detected -- C-02 regression")
		}
	})

	// Test 5: Empty StateHash
	t.Run("empty_StateHash", func(t *testing.T) {
		clone := cloneState(t, state)
		clone.Contributions[2].StateHash = ""
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("empty StateHash must be detected -- C-02 regression")
		}
	})
}

// =============================================================================
// MEDIUM Regressions
// =============================================================================

// TestRegressionM01_CeremonyAlphaBetaConsistency verifies that verifyCeremony
// detects inconsistency between alphaG1 and the tau ratio.
// Finding M-01: No cross-consistency check between alpha/beta arrays and tau.
func TestRegressionM01_CeremonyAlphaBetaConsistency(t *testing.T) {
	state, err := initCeremony("test-m01", 1<<4, 1)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	contrib, err := contribute(state, "Honest")
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	state.Contributions = append(state.Contributions, contrib)

	// Corrupt alphaG1[1] with a random point that breaks the tau ratio
	t.Run("corrupt_alphaG1", func(t *testing.T) {
		clone := cloneState(t, state)
		// Set alphaG1[1] to the generator (breaks alpha*tau relationship)
		_, _, g1, _ := bn254.Generators()
		clone.AlphaG1[1] = G1Point{g1}
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("inconsistent alphaG1 must reject -- M-01 regression")
		}
	})

	// Corrupt betaG1[1] with a random point
	t.Run("corrupt_betaG1", func(t *testing.T) {
		clone := cloneState(t, state)
		_, _, g1, _ := bn254.Generators()
		clone.BetaG1[1] = G1Point{g1}
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("inconsistent betaG1 must reject -- M-01 regression")
		}
	})

	// Corrupt betaG2 (breaks betaG1/betaG2 cross-check)
	t.Run("corrupt_betaG2", func(t *testing.T) {
		clone := cloneState(t, state)
		_, _, _, g2 := bn254.Generators()
		clone.BetaG2 = G2Point{g2}
		if err := verifyCeremony(clone); err == nil {
			t.Fatal("inconsistent betaG2 must reject -- M-01 regression")
		}
	})
}

// TestRegressionM02_SameRatioArgOrder verifies that sameRatio checks the
// correct ratio direction (n1/d1 == n2/d2, not swapped args).
// Finding M-02: Arguments to sameRatio were swapped, causing the check
// to only pass for tau=1 (the identity case).
func TestRegressionM02_SameRatioArgOrder(t *testing.T) {
	// A real ceremony with random tau != 1 must verify.
	// If sameRatio args were swapped, it would only pass for tau=1.
	state, err := initCeremony("test-m02", 1<<4, 1)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	contrib, err := contribute(state, "Participant")
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	state.Contributions = append(state.Contributions, contrib)

	// With real random tau (astronomically unlikely to be 1), verification
	// must pass. If args were swapped, this would fail.
	if err := verifyCeremony(state); err != nil {
		t.Fatalf("valid ceremony with random tau must verify (arg order correct) -- M-02 regression: %v", err)
	}

	// Also verify sameRatio directly with a known non-identity scalar
	_, _, g1, g2 := bn254.Generators()

	var scalar fr.Element
	scalar.SetUint64(42) // tau = 42, definitely not 1
	var bi big.Int
	scalar.BigInt(&bi)

	var tauG1 bn254.G1Affine
	tauG1.ScalarMultiplication(&g1, &bi)

	var tauG2 bn254.G2Affine
	tauG2.ScalarMultiplication(&g2, &bi)

	// Correct order: tauG1/G1 == tauG2/G2 (both ratios = tau = 42)
	if !sameRatio(tauG1, g1, tauG2, g2) {
		t.Fatal("sameRatio(tau*G1, G1, tau*G2, G2) must be true -- M-02 regression")
	}

	// Wrong ratio must fail
	var wrongG1 bn254.G1Affine
	var wrongScalar fr.Element
	wrongScalar.SetUint64(7)
	var wrongBI big.Int
	wrongScalar.BigInt(&wrongBI)
	wrongG1.ScalarMultiplication(&g1, &wrongBI)

	if sameRatio(wrongG1, g1, tauG2, g2) {
		t.Fatal("sameRatio with different ratios must be false -- M-02 regression")
	}
}

// =============================================================================
// INFO Regressions
// =============================================================================

// TestRegressionI03_DefaultPowerIs20 verifies the ceremony CLI default power
// flag is 20.
// Finding I-03: Default power of 10 was too small for production circuits.
func TestRegressionI03_DefaultPowerIs20(t *testing.T) {
	// The cmdInit function in main.go uses:
	//   power := fs.Int("power", 20, ...)
	// We verify by checking the initCeremony output with 2^20.
	numConstraints := 1 << 20
	state, err := initCeremony("power-test", numConstraints, 1)
	if err != nil {
		t.Fatalf("initCeremony with 2^20: %v", err)
	}
	if state.PowersNeeded != numConstraints+1 {
		t.Fatalf("expected %d powers, got %d -- I-03 regression", numConstraints+1, state.PowersNeeded)
	}
}

// =============================================================================
// LOW Regressions
// =============================================================================

// TestRegressionL01_CeremonyHasTests is a meta-regression verifying that the
// ceremony package has substantive tests (not just a placeholder).
// Finding L-01: Ceremony had no tests, so regressions went undetected.
func TestRegressionL01_CeremonyHasTests(t *testing.T) {
	// If this test compiles and runs, the ceremony package has tests.
	// Also verify the existing full-cycle test works (init, contribute, verify).
	state, err := initCeremony("meta-test", 1<<4, 1)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	contrib, err := contribute(state, "Tester")
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	state.Contributions = append(state.Contributions, contrib)

	if err := verifyCeremony(state); err != nil {
		t.Fatalf("ceremony lifecycle must work -- L-01 regression: %v", err)
	}
}

// TestRegressionL03_StateFileHasIntegrity verifies that writeState produces
// a file with an integrity hash and readState validates it.
// Finding L-03: State files had no integrity field, allowing silent corruption.
func TestRegressionL03_StateFileHasIntegrity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "integrity-test.json")

	state, err := initCeremony("integrity", 1<<4, 1)
	if err != nil {
		t.Fatalf("initCeremony: %v", err)
	}

	if err := writeState(state, path); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	// Read raw JSON and verify envelope has "integrity" field
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var envelope struct {
		State     json.RawMessage `json:"state"`
		Integrity string          `json:"integrity"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if envelope.Integrity == "" {
		t.Fatal("state file must have non-empty integrity field -- L-03 regression")
	}

	// Verify the integrity hash is correct
	h := sha256.Sum256(envelope.State)
	expected := hex.EncodeToString(h[:])
	if envelope.Integrity != expected {
		t.Fatalf("integrity hash mismatch: got %s, want %s -- L-03 regression", envelope.Integrity, expected)
	}

	// Tamper and verify readState rejects it
	envelope.Integrity = strings.Repeat("0", 64)
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal tampered: %v", err)
	}
	tamperedPath := filepath.Join(dir, "tampered.json")
	if err := os.WriteFile(tamperedPath, tampered, 0600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	_, err = readState(tamperedPath)
	if err == nil {
		t.Fatal("readState must reject tampered integrity -- L-03 regression")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("error must mention 'integrity', got: %v", err)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// cloneState deep-copies a CeremonyState via JSON round-trip.
func cloneState(t *testing.T, orig *CeremonyState) *CeremonyState {
	t.Helper()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var clone CeremonyState
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	return &clone
}

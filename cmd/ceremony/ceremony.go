package main

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

// initCeremony creates the initial ceremony state with powers of the generators.
// This is the "toxic waste" starting point -- before any real randomness is mixed in.
func initCeremony(circuit string, numConstraints, participants int) (*CeremonyState, error) {
	// Powers needed = numConstraints + 1 (for Groth16 SRS)
	n := numConstraints + 1

	_, _, g1, g2 := bn254.Generators()

	// tau^i * G1 for i in [0, n)
	tauG1 := make([]G1Point, n)
	tauG1[0] = G1Point{g1}
	for i := 1; i < n; i++ {
		tauG1[i] = G1Point{g1} // initially all just G1 generator
	}

	// tau^i * G2 for i in [0, 2) -- Groth16 only needs tau^0 and tau^1 in G2
	tauG2 := make([]G2Point, 2)
	tauG2[0] = G2Point{g2}
	tauG2[1] = G2Point{g2}

	// alpha * tau^i * G1
	alphaG1 := make([]G1Point, n)
	for i := range alphaG1 {
		alphaG1[i] = G1Point{g1}
	}

	// beta * tau^i * G1
	betaG1 := make([]G1Point, n)
	for i := range betaG1 {
		betaG1[i] = G1Point{g1}
	}

	// beta * G2
	betaG2 := G2Point{g2}

	return &CeremonyState{
		Circuit:        circuit,
		NumConstraints: numConstraints,
		PowersNeeded:   n,
		Participants:   participants,
		TauG1:          tauG1,
		TauG2:          tauG2,
		AlphaG1:        alphaG1,
		BetaG1:         betaG1,
		BetaG2:         betaG2,
	}, nil
}

// contribute applies a participant's random contribution to the ceremony state.
// Returns the contribution record. The random scalars are zeroed after use.
func contribute(state *CeremonyState, participant string) (Contribution, error) {
	// Generate random scalars from crypto/rand
	var tau, alpha, beta fr.Element
	if _, err := tau.SetRandom(); err != nil {
		return Contribution{}, fmt.Errorf("generate tau: %w", err)
	}
	if _, err := alpha.SetRandom(); err != nil {
		return Contribution{}, fmt.Errorf("generate alpha: %w", err)
	}
	if _, err := beta.SetRandom(); err != nil {
		return Contribution{}, fmt.Errorf("generate beta: %w", err)
	}

	// Commitment hash: SHA-256(tau || alpha || beta) before they're used
	commitHash := commitmentHash(&tau, &alpha, &beta)

	var tauBI, alphaBI, betaBI big.Int
	tau.BigInt(&tauBI)
	alpha.BigInt(&alphaBI)
	beta.BigInt(&betaBI)

	// Update tauG1: tauG1[i] *= tau^i
	// We accumulate tau powers: tau^0=1, tau^1, tau^2, ...
	var tauPow big.Int
	tauPow.SetInt64(1)
	for i := range state.TauG1 {
		state.TauG1[i].ScalarMultiplication(&state.TauG1[i].G1Affine, &tauPow)
		if i < len(state.TauG1)-1 {
			tauPow.Mul(&tauPow, &tauBI)
			tauPow.Mod(&tauPow, fr.Modulus())
		}
	}

	// Update tauG2: tauG2[i] *= tau^i
	tauPow.SetInt64(1)
	for i := range state.TauG2 {
		state.TauG2[i].ScalarMultiplication(&state.TauG2[i].G2Affine, &tauPow)
		if i < len(state.TauG2)-1 {
			tauPow.Mul(&tauPow, &tauBI)
			tauPow.Mod(&tauPow, fr.Modulus())
		}
	}

	// Update alphaG1: alphaG1[i] *= alpha * tau^i
	var scale big.Int
	tauPow.SetInt64(1)
	for i := range state.AlphaG1 {
		scale.Mul(&alphaBI, &tauPow)
		scale.Mod(&scale, fr.Modulus())
		state.AlphaG1[i].ScalarMultiplication(&state.AlphaG1[i].G1Affine, &scale)
		if i < len(state.AlphaG1)-1 {
			tauPow.Mul(&tauPow, &tauBI)
			tauPow.Mod(&tauPow, fr.Modulus())
		}
	}

	// Update betaG1: betaG1[i] *= beta * tau^i
	tauPow.SetInt64(1)
	for i := range state.BetaG1 {
		scale.Mul(&betaBI, &tauPow)
		scale.Mod(&scale, fr.Modulus())
		state.BetaG1[i].ScalarMultiplication(&state.BetaG1[i].G1Affine, &scale)
		if i < len(state.BetaG1)-1 {
			tauPow.Mul(&tauPow, &tauBI)
			tauPow.Mod(&tauPow, fr.Modulus())
		}
	}

	// Update betaG2: betaG2 *= beta
	state.BetaG2.ScalarMultiplication(&state.BetaG2.G2Affine, &betaBI)

	// Zero all secret scalars: fr.Elements, big.Ints, and intermediates
	tau.SetZero()
	alpha.SetZero()
	beta.SetZero()
	zeroBI(&tauBI)
	zeroBI(&alphaBI)
	zeroBI(&betaBI)
	zeroBI(&tauPow)
	zeroBI(&scale)

	// Overwrite stack memory with random bytes
	var garbage [32]byte
	rand.Read(garbage[:])

	// Compute hash chain fields
	prevHash := ""
	if len(state.Contributions) > 0 {
		prevHash = state.Contributions[len(state.Contributions)-1].StateHash
	}
	stateHash := computeStateHash(state)

	return newContribution(participant, commitHash, prevHash, stateHash), nil
}

// verifyCeremony checks the consistency of the ceremony state.
// It verifies that tauG1 and tauG2 form consistent geometric sequences
// using pairing checks: e(tauG1[i], tauG2[0]) == e(tauG1[i-1], tauG2[1])
func verifyCeremony(state *CeremonyState) error {
	if len(state.TauG1) < 2 {
		return fmt.Errorf("need at least 2 powers, got %d", len(state.TauG1))
	}
	if len(state.TauG2) < 2 {
		return fmt.Errorf("need 2 G2 powers, got %d", len(state.TauG2))
	}
	if len(state.Contributions) == 0 {
		return fmt.Errorf("no contributions recorded")
	}

	// Check tauG1 forms a geometric sequence with ratio matching tauG2.
	// We want tauG1[i+1]/tauG1[i] == tauG2[1]/tauG2[0] == tau.
	// sameRatio(n1, d1, n2, d2) checks n1/d1 == n2/d2 via e(n1,d2)*e(-d1,n2)==1.
	for i := 0; i < len(state.TauG1)-1; i++ {
		if !sameRatio(
			state.TauG1[i+1].G1Affine, state.TauG1[i].G1Affine,
			state.TauG2[1].G2Affine, state.TauG2[0].G2Affine,
		) {
			return fmt.Errorf("tauG1 consistency check failed at index %d", i)
		}
	}

	// Check alphaG1 has same tau ratio between consecutive elements.
	for i := 0; i < len(state.AlphaG1)-1; i++ {
		if !sameRatio(
			state.AlphaG1[i+1].G1Affine, state.AlphaG1[i].G1Affine,
			state.TauG2[1].G2Affine, state.TauG2[0].G2Affine,
		) {
			return fmt.Errorf("alphaG1 consistency check failed at index %d", i)
		}
	}

	// Check betaG1 has same tau ratio between consecutive elements.
	for i := 0; i < len(state.BetaG1)-1; i++ {
		if !sameRatio(
			state.BetaG1[i+1].G1Affine, state.BetaG1[i].G1Affine,
			state.TauG2[1].G2Affine, state.TauG2[0].G2Affine,
		) {
			return fmt.Errorf("betaG1 consistency check failed at index %d", i)
		}
	}

	// Cross-consistency: verify alphaG1 uses the same tau ratio as tauG1/tauG2
	// alphaG1[1]/alphaG1[0] must equal tauG2[1]/tauG2[0] (same tau)
	if len(state.AlphaG1) >= 2 {
		if !sameRatio(
			state.AlphaG1[1].G1Affine, state.AlphaG1[0].G1Affine,
			state.TauG2[1].G2Affine, state.TauG2[0].G2Affine,
		) {
			return fmt.Errorf("alpha-tau cross-consistency check failed")
		}
	}

	// Cross-consistency: verify betaG1[0] and betaG2 encode the same beta scalar
	// betaG1[0]/G1Gen must equal betaG2/G2Gen
	var g1Gen bn254.G1Affine
	var g2Gen bn254.G2Affine
	_, _, g1Gen, g2Gen = bn254.Generators()
	if !sameRatio(
		state.BetaG1[0].G1Affine, g1Gen,
		state.BetaG2.G2Affine, g2Gen,
	) {
		return fmt.Errorf("betaG1/betaG2 cross-consistency check failed")
	}

	// Check none of the key elements are the point at infinity
	if state.TauG1[0].IsInfinity() || state.TauG2[0].IsInfinity() {
		return fmt.Errorf("generator points are at infinity")
	}
	if state.BetaG2.IsInfinity() {
		return fmt.Errorf("betaG2 is at infinity")
	}

	// Verify contribution hash chain
	for i, c := range state.Contributions {
		if i == 0 {
			if c.PrevHash != "" {
				return fmt.Errorf("first contribution has non-empty PrevHash")
			}
		} else {
			if c.PrevHash != state.Contributions[i-1].StateHash {
				return fmt.Errorf("contribution %d: PrevHash does not match previous StateHash", i)
			}
		}
		if c.StateHash == "" {
			return fmt.Errorf("contribution %d: empty StateHash", i)
		}
	}

	// Verify final StateHash matches current SRS state
	finalStateHash := computeStateHash(state)
	lastContrib := state.Contributions[len(state.Contributions)-1]
	if lastContrib.StateHash != finalStateHash {
		return fmt.Errorf("final contribution StateHash does not match current SRS state")
	}

	return nil
}

// exportSRS writes the binary SRS (tauG1, tauG2, alphaG1, betaG1, betaG2)
// in uncompressed form for use by the Groth16 prover/verifier.
func exportSRS(state *CeremonyState) []byte {
	// Format: [4 bytes n][tauG1...][tauG2...][alphaG1...][betaG1...][betaG2]
	// All points in uncompressed form (G1: 64 bytes, G2: 128 bytes)
	n := len(state.TauG1)
	// Header: 4 bytes for n
	// tauG1: n * 64
	// tauG2: 2 * 128
	// alphaG1: n * 64
	// betaG1: n * 64
	// betaG2: 1 * 128
	size := 4 + n*64 + 2*128 + n*64 + n*64 + 128
	buf := make([]byte, 0, size)

	// Write n as 4-byte big-endian
	buf = append(buf, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))

	for i := range state.TauG1 {
		buf = append(buf, state.TauG1[i].Marshal()...)
	}
	for i := range state.TauG2 {
		buf = append(buf, state.TauG2[i].Marshal()...)
	}
	for i := range state.AlphaG1 {
		buf = append(buf, state.AlphaG1[i].Marshal()...)
	}
	for i := range state.BetaG1 {
		buf = append(buf, state.BetaG1[i].Marshal()...)
	}
	buf = append(buf, state.BetaG2.Marshal()...)

	return buf
}

// sameRatio checks n1/d1 == n2/d2 via pairing: e(n1, d2) == e(d1, n2)
// Specifically: e(n1, d2) * e(-d1, n2) == 1
func sameRatio(n1, d1 bn254.G1Affine, n2, d2 bn254.G2Affine) bool {
	var negD1 bn254.G1Affine
	negD1.Neg(&d1)
	ok, err := bn254.PairingCheck(
		[]bn254.G1Affine{n1, negD1},
		[]bn254.G2Affine{d2, n2},
	)
	if err != nil {
		return false
	}
	return ok
}

// zeroBI overwrites a big.Int's internal limbs with zeros.
func zeroBI(x *big.Int) {
	if bits := x.Bits(); bits != nil {
		for i := range bits {
			bits[i] = 0
		}
	}
	x.SetInt64(0)
}

// commitmentHash returns SHA-256(tau.Bytes() || alpha.Bytes() || beta.Bytes())
func commitmentHash(tau, alpha, beta *fr.Element) []byte {
	h := sha256.New()
	var buf [32]byte
	tb := tau.Bytes()
	copy(buf[:], tb[:])
	h.Write(buf[:])
	ab := alpha.Bytes()
	copy(buf[:], ab[:])
	h.Write(buf[:])
	bb := beta.Bytes()
	copy(buf[:], bb[:])
	h.Write(buf[:])
	return h.Sum(nil)
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/mldsa"
	"github.com/stretchr/testify/require"
)

// makeRandomBytes returns n random bytes.
func makeRandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// makeValidBLSEntry returns (msg, sig, pk) for a REAL BLS signature over a
// random 32-byte message: pk is the 48-byte compressed G1 key, sig is the
// 96-byte compressed G2 signature. The CPU oracle must accept this.
func makeValidBLSEntry(t testing.TB) (msg, sig, pk []byte) {
	t.Helper()
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	msg = makeRandomBytes(32)
	s, err := sk.Sign(msg)
	require.NoError(t, err)
	sig = bls.SignatureToBytes(s)
	pk = bls.PublicKeyToCompressedBytes(sk.PublicKey())
	require.Len(t, sig, 96)
	require.Len(t, pk, 48)
	return msg, sig, pk
}

// makeValidMLDSAEntry returns (msg, sig, pk) for a REAL ML-DSA-65 signature
// over a random 64-byte message. The sizes are taken from the crypto package
// constants (FIPS-204 ML-DSA-65: pk 1952 bytes, sig 3309 bytes) rather than
// hard-coded — see TestMLDSA_WorkStructSizeIsCanonical, which holds the
// MLDSAWork struct / gpuMLDSAVerify sig constant pinned at the FIPS-204 3309
// (corrected from the stale round-3 Dilithium3 3293). The CPU oracle uses the
// typed Verify, so it accepts the real signature regardless.
func makeValidMLDSAEntry(t testing.TB) (msg, sig, pk []byte) {
	t.Helper()
	priv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(t, err)
	msg = makeRandomBytes(64)
	sig, err = priv.Sign(rand.Reader, msg, nil)
	require.NoError(t, err)
	pk = priv.PublicKey.Bytes()
	require.Len(t, sig, mldsa.MLDSA65SignatureSize)
	require.Len(t, pk, mldsa.MLDSA65PublicKeySize)
	return msg, sig, pk
}

// makeBLSWork creates BLSWork with n entries carrying REAL valid BLS
// signatures (the CPU oracle now performs real verification).
func makeBLSWork(t testing.TB, n int) *BLSWork {
	t.Helper()
	w := &BLSWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i], w.Signatures[i], w.PubKeys[i] = makeValidBLSEntry(t)
	}
	return w
}

// makeCoronaWork creates CoronaWork with n entries.
func makeCoronaWork(n int) *CoronaWork {
	w := &CoronaWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i] = makeRandomBytes(48)
		w.Signatures[i] = makeRandomBytes(512)
		w.PubKeys[i] = makeRandomBytes(256)
	}
	return w
}

// makeZKWork creates ZKWork with m entries.
func makeZKWork(m int) *ZKWork {
	w := &ZKWork{
		Scalars: make([][]byte, m),
		Bases:   make([][]byte, m),
	}
	for i := 0; i < m; i++ {
		w.Scalars[i] = makeRandomBytes(32)
		w.Bases[i] = makeRandomBytes(64)
	}
	return w
}

// makeMLDSAWork creates MLDSAWork with n entries carrying REAL valid
// ML-DSA-65 (Dilithium3) signatures (the CPU oracle now performs real
// verification).
func makeMLDSAWork(t testing.TB, n int) *MLDSAWork {
	t.Helper()
	w := &MLDSAWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i], w.Signatures[i], w.PubKeys[i] = makeValidMLDSAEntry(t)
	}
	return w
}

func TestGPUPipeline_AllFourTypes(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:    makeBLSWork(t, 5),
		Corona: makeCoronaWork(3),
		ZK:     makeZKWork(2),
		MLDSA:  makeMLDSAWork(t, 10),
	}

	result, err := pipeline.VerifyBlock(work)
	require.NoError(t, err)
	require.NotNil(t, result)

	// BLS results: real valid signatures, CPU oracle accepts.
	require.Len(t, result.BLSValid, 5, "should have 5 BLS results")
	for i, v := range result.BLSValid {
		require.True(t, v, "BLS[%d] should be valid", i)
	}

	// Corona results: no pure-Go Corona verifier exists, so the CPU oracle
	// fails closed — every element is rejected (never rubber-stamped).
	require.Len(t, result.CoronaValid, 3, "should have 3 Corona results")
	for i, v := range result.CoronaValid {
		require.False(t, v, "Corona[%d] must fail closed (no pure-Go verifier)", i)
	}

	// ZK result: no pure-Go ZK verifier exists, so the CPU oracle fails closed.
	require.False(t, result.ZKValid, "ZK batch must fail closed (no pure-Go verifier)")

	// ML-DSA results: real valid signatures, CPU oracle accepts.
	require.Len(t, result.MLDSAValid, 10, "should have 10 ML-DSA results")
	for i, v := range result.MLDSAValid {
		require.True(t, v, "MLDSA[%d] should be valid", i)
	}

	// Timing: all durations should be non-negative
	require.GreaterOrEqual(t, result.TotalTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.BLSTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.CoronaTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.ZKTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.MLDSATime.Nanoseconds(), int64(0))

	// Stats should reflect the verification
	stats := pipeline.Stats()
	require.Equal(t, uint64(1), stats.GPUVerifies+stats.CPUVerifies,
		"exactly one verify should have been recorded")
}

func TestGPUPipeline_CPUFallback(t *testing.T) {
	// Without CGO/GPU, accel.Available() returns false.
	// Pipeline must fall back to CPU verification.
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:   makeBLSWork(t, 3),
		MLDSA: makeMLDSAWork(t, 4),
	}

	result, err := pipeline.VerifyBlock(work)
	require.NoError(t, err)
	require.NotNil(t, result)

	// CPU fallback performs real verification; valid signatures are accepted.
	require.Len(t, result.BLSValid, 3)
	for i, v := range result.BLSValid {
		require.True(t, v, "CPU BLS[%d] should be valid", i)
	}

	require.Len(t, result.MLDSAValid, 4)
	for i, v := range result.MLDSAValid {
		require.True(t, v, "CPU MLDSA[%d] should be valid", i)
	}

	// GPU should not have been used (no CGO in test env)
	require.False(t, result.GPUUsed, "should use CPU fallback")

	stats := pipeline.Stats()
	require.Equal(t, uint64(1), stats.CPUVerifies)
}

// TestCPUVerify_RealOracle proves the CPU fallback is a real cryptographic
// oracle, not a length-checking rubber stamp: a valid signature is accepted
// and a well-formed-but-FORGED signature (correct lengths, wrong bytes) is
// REJECTED. The forged-rejection case is the regression guard against the
// old `return true for well-formed inputs` behavior.
func TestCPUVerify_RealOracle(t *testing.T) {
	t.Run("BLS valid accepted, forged rejected", func(t *testing.T) {
		msg, sig, pk := makeValidBLSEntry(t)

		// Valid signature => accepted.
		good := cpuBLSVerify(&BLSWork{
			Messages:   [][]byte{msg},
			Signatures: [][]byte{sig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{true}, good, "valid BLS signature must be accepted")

		// Forged signature: correct 96-byte length, random bytes => rejected.
		forgedSig := makeRandomBytes(96)
		bad := cpuBLSVerify(&BLSWork{
			Messages:   [][]byte{msg},
			Signatures: [][]byte{forgedSig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{false}, bad, "forged BLS signature (right length, wrong bytes) must be REJECTED")

		// Valid signature against the WRONG message => rejected.
		wrongMsg := cpuBLSVerify(&BLSWork{
			Messages:   [][]byte{makeRandomBytes(32)},
			Signatures: [][]byte{sig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{false}, wrongMsg, "BLS signature over a different message must be REJECTED")
	})

	t.Run("MLDSA valid accepted, forged rejected", func(t *testing.T) {
		msg, sig, pk := makeValidMLDSAEntry(t)

		// Valid signature => accepted.
		good := cpuMLDSAVerify(&MLDSAWork{
			Messages:   [][]byte{msg},
			Signatures: [][]byte{sig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{true}, good, "valid ML-DSA signature must be accepted")

		// Forged signature: correct length, random bytes => rejected.
		forgedSig := makeRandomBytes(mldsa.MLDSA65SignatureSize)
		bad := cpuMLDSAVerify(&MLDSAWork{
			Messages:   [][]byte{msg},
			Signatures: [][]byte{forgedSig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{false}, bad, "forged ML-DSA signature (right length, wrong bytes) must be REJECTED")

		// Valid signature against the WRONG message => rejected.
		wrongMsg := cpuMLDSAVerify(&MLDSAWork{
			Messages:   [][]byte{makeRandomBytes(64)},
			Signatures: [][]byte{sig},
			PubKeys:    [][]byte{pk},
		})
		require.Equal(t, []bool{false}, wrongMsg, "ML-DSA signature over a different message must be REJECTED")
	})

	t.Run("Corona fails closed", func(t *testing.T) {
		// No pure-Go Corona verifier exists; every element must be rejected,
		// never rubber-stamped on length alone.
		got := cpuCoronaVerify(&CoronaWork{
			Messages:   [][]byte{makeRandomBytes(48), makeRandomBytes(48)},
			Signatures: [][]byte{makeRandomBytes(512), makeRandomBytes(512)},
			PubKeys:    [][]byte{makeRandomBytes(256), makeRandomBytes(256)},
		})
		require.Equal(t, []bool{false, false}, got, "Corona must fail closed for all elements")
	})

	t.Run("ZK fails closed", func(t *testing.T) {
		// No pure-Go ZK proof verifier exists; the batch must be rejected.
		got := cpuZKVerify(&ZKWork{
			Scalars: [][]byte{makeRandomBytes(32)},
			Bases:   [][]byte{makeRandomBytes(64)},
		})
		require.False(t, got, "ZK must fail closed")
	})
}

// TestMLDSA_WorkStructSizeIsCanonical pins the FIPS-204 ML-DSA-65 signature and
// public key sizes that the MLDSAWork struct comment and gpuMLDSAVerify's
// fixed-size flatten now use. luxfi/crypto (circl v1.6.3, FIPS-204 final)
// produces 3309-byte ML-DSA-65 signatures (5*640 + 55 + 6 + 48 = 3309); the
// GPU flatten width was corrected from the stale round-3 Dilithium3 size 3293
// to 3309 so the GPU path no longer clamps/corrupts a real signature. The
// public key size (1952) is unchanged across the round-3 -> final transition.
//
// This is the equivalence-pair guard: the CPU oracle (cpuMLDSAVerify) parses
// pk via the typed PublicKeyFromBytes and calls VerifySignature, accepting the
// real 3309-byte signature; the GPU path sizes the signature identically. If
// the crypto constant ever drifts, this test fails before any divergence
// reaches the GPU ML-DSA kernel (not yet wired into block-accept).
func TestMLDSA_WorkStructSizeIsCanonical(t *testing.T) {
	require.Equal(t, 1952, mldsa.MLDSA65PublicKeySize,
		"ML-DSA-65 public key is 1952 bytes (matches MLDSAWork / gpuMLDSAVerify pkLen)")
	require.Equal(t, 3309, mldsa.MLDSA65SignatureSize,
		"ML-DSA-65 signature is 3309 bytes (FIPS-204), matching the MLDSAWork struct / gpuMLDSAVerify sigLen")
}

func TestGPUPipeline_EmptyBatches(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	tests := []struct {
		name string
		work *BlockVerifyWork
	}{
		{
			name: "nil work",
			work: nil,
		},
		{
			name: "all nil batches",
			work: &BlockVerifyWork{},
		},
		{
			name: "empty BLS only",
			work: &BlockVerifyWork{
				BLS: &BLSWork{},
			},
		},
		{
			name: "BLS filled, rest nil",
			work: &BlockVerifyWork{
				BLS: makeBLSWork(t, 2),
			},
		},
		{
			name: "ZK only",
			work: &BlockVerifyWork{
				ZK: makeZKWork(1),
			},
		},
		{
			name: "MLDSA only",
			work: &BlockVerifyWork{
				MLDSA: makeMLDSAWork(t, 1),
			},
		},
		{
			name: "Corona only",
			work: &BlockVerifyWork{
				Corona: makeCoronaWork(1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.VerifyBlock(tt.work)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestGPUPipeline_ValidationErrors(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	tests := []struct {
		name    string
		work    *BlockVerifyWork
		wantErr error
	}{
		{
			name: "BLS size mismatch",
			work: &BlockVerifyWork{
				BLS: &BLSWork{
					Messages:   [][]byte{{1}},
					Signatures: [][]byte{{1}, {2}}, // 2 != 1
					PubKeys:    [][]byte{{1}},
				},
			},
			wantErr: ErrBLSSizeMismatch,
		},
		{
			name: "Corona size mismatch",
			work: &BlockVerifyWork{
				Corona: &CoronaWork{
					Messages:   [][]byte{{1}, {2}},
					Signatures: [][]byte{{1}}, // 1 != 2
					PubKeys:    [][]byte{{1}, {2}},
				},
			},
			wantErr: ErrCoronaSizeMismatch,
		},
		{
			name: "ZK size mismatch",
			work: &BlockVerifyWork{
				ZK: &ZKWork{
					Scalars: [][]byte{{1}, {2}},
					Bases:   [][]byte{{1}}, // 1 != 2
				},
			},
			wantErr: ErrZKSizeMismatch,
		},
		{
			name: "MLDSA size mismatch",
			work: &BlockVerifyWork{
				MLDSA: &MLDSAWork{
					Messages:   [][]byte{{1}},
					Signatures: [][]byte{{1}},
					PubKeys:    [][]byte{{1}, {2}}, // 2 != 1
				},
			},
			wantErr: ErrMLDSASizeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.VerifyBlock(tt.work)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func BenchmarkGPUPipeline(b *testing.B) {
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:    makeBLSWork(b, 100),
		Corona: makeCoronaWork(50),
		ZK:     makeZKWork(10),
		MLDSA:  makeMLDSAWork(b, 200),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.VerifyBlock(work)
	}
}

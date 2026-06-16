// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/xvm/state/xvmroot"
)

// randUTXOLeaves builds n UTXO leaves with random fields and a random occupancy
// mix (roughly half unoccupied) so the parallel compaction is exercised against
// the serial one over a non-trivial occupied subset.
func randUTXOLeaves(rng *rand.Rand, n int) []xvmroot.UTXOLeaf {
	us := make([]xvmroot.UTXOLeaf, n)
	for i := range us {
		rng.Read(us[i].UTXOID[:])
		rng.Read(us[i].AssetID[:])
		rng.Read(us[i].OwnerRoot[:])
		us[i].AmountLo = rng.Uint64()
		us[i].AmountHi = rng.Uint64()
		us[i].Locktime = rng.Uint64()
		us[i].Threshold = rng.Uint32()
		if rng.Intn(2) == 0 {
			us[i].Status = xvmroot.UTXOOccupied
		}
	}
	return us
}

func randAssetLeaves(rng *rand.Rand, n int) []xvmroot.AssetLeaf {
	as := make([]xvmroot.AssetLeaf, n)
	for i := range as {
		rng.Read(as[i].AssetID[:])
		rng.Read(as[i].MintAuthorityRoot[:])
		as[i].TotalSupplyLo = rng.Uint64()
		as[i].TotalSupplyHi = rng.Uint64()
		as[i].FreezeFlag = rng.Uint32()
		as[i].Denomination = rng.Uint32()
		if rng.Intn(2) == 0 {
			as[i].Occupied = 1
		}
	}
	return as
}

func randTxLeaves(rng *rand.Rand, n int) []xvmroot.TxLeaf {
	ts := make([]xvmroot.TxLeaf, n)
	for i := range ts {
		rng.Read(ts[i].TxID[:])
		rng.Read(ts[i].ProofDigest[:])
		ts[i].Kind = rng.Uint32()
		ts[i].Status = rng.Uint32()
		ts[i].RejectReason = rng.Uint32()
	}
	return ts
}

// TestParallelFamilyRootsMatchSerial pins each parallel family fold byte-for-byte
// against the serial xvmroot family root, across a range of sizes that includes
// 0, 1, odd, even, and sizes straddling the inline/worker threshold — the leaf
// counts where lone-right Merkle promotion and worker sharding edges live.
func TestParallelFamilyRootsMatchSerial(t *testing.T) {
	require := require.New(t)
	rng := rand.New(rand.NewSource(0xC0FFEE))

	sizes := []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 31, 33, 63, 64, 65,
		127, 255, 256, 257, 511, 1000, 1023, 1024, 4097}
	for _, n := range sizes {
		us := randUTXOLeaves(rng, n)
		require.Equalf(xvmroot.UTXORoot(us), parallelUTXORoot(us), "utxo root n=%d", n)

		as := randAssetLeaves(rng, n)
		require.Equalf(xvmroot.AssetRoot(as), parallelAssetRoot(as), "asset root n=%d", n)

		ts := randTxLeaves(rng, n)
		require.Equalf(xvmroot.TxRoot(ts), parallelTxRoot(ts), "tx root n=%d", n)
	}
}

// TestParallelExecutionRootMatchesSerial is the property test the deliverable
// requires: the full parallel execution_root equals the serial
// xvmroot.ExecutionRoot over random state of random sizes (including odd
// per-family counts), so the parallel build is a drop-in for the serial oracle.
func TestParallelExecutionRootMatchesSerial(t *testing.T) {
	require := require.New(t)
	rng := rand.New(rand.NewSource(0x5EED))

	for iter := 0; iter < 200; iter++ {
		var parent [xvmroot.Size]byte
		rng.Read(parent[:])
		height := rng.Uint64()

		// Independent, frequently-odd per-family sizes.
		nU := rng.Intn(600)
		nA := rng.Intn(600)
		nT := rng.Intn(600)
		us := randUTXOLeaves(rng, nU)
		as := randAssetLeaves(rng, nA)
		ts := randTxLeaves(rng, nT)

		wantExec, wantU, wantA, wantT := xvmroot.ExecutionRoot(parent, us, as, ts, height)
		gotExec, gotU, gotA, gotT := xvmExecutionRoot(parent, us, as, ts, height)

		require.Equalf(wantU, gotU, "utxo iter=%d nU=%d", iter, nU)
		require.Equalf(wantA, gotA, "asset iter=%d nA=%d", iter, nA)
		require.Equalf(wantT, gotT, "tx iter=%d nT=%d", iter, nT)
		require.Equalf(wantExec, gotExec, "exec iter=%d", iter)
	}
}

// TestParallelExecutionRootEmpty confirms the all-empty state reduces to the
// same composed root the serial path yields (empty family roots are
// keccak256(""), not zero).
func TestParallelExecutionRootEmpty(t *testing.T) {
	require := require.New(t)
	var parent [xvmroot.Size]byte
	wantExec, _, _, _ := xvmroot.ExecutionRoot(parent, nil, nil, nil, 0)
	gotExec, _, _, _ := xvmExecutionRoot(parent, nil, nil, nil, 0)
	require.Equal(wantExec, gotExec)
}

const benchUTXOCount = 65536

func benchUTXOSet() []xvmroot.UTXOLeaf {
	rng := rand.New(rand.NewSource(1))
	us := make([]xvmroot.UTXOLeaf, benchUTXOCount)
	for i := range us {
		rng.Read(us[i].UTXOID[:])
		rng.Read(us[i].AssetID[:])
		rng.Read(us[i].OwnerRoot[:])
		us[i].AmountLo = rng.Uint64()
		us[i].Threshold = 1
		us[i].Status = xvmroot.UTXOOccupied // all occupied: worst-case fold width
	}
	return us
}

// BenchmarkUTXORootSerial folds a 65536-leaf UTXO set with the serial
// xvmroot.UTXORoot.
func BenchmarkUTXORootSerial(b *testing.B) {
	us := benchUTXOSet()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = xvmroot.UTXORoot(us)
	}
}

// BenchmarkUTXORootParallel folds the same 65536-leaf set with the parallel
// worker-pool fold. Compare against BenchmarkUTXORootSerial to see the
// leaf-hashing speedup.
func BenchmarkUTXORootParallel(b *testing.B) {
	us := benchUTXOSet()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parallelUTXORoot(us)
	}
}

// BenchmarkExecutionRootSerial / Parallel compare the full execution_root build
// over a 65536-UTXO post-block state (with a modest asset + tx set), serial
// xvmroot.ExecutionRoot vs the concurrent xvmExecutionRoot.
func BenchmarkExecutionRootSerial(b *testing.B) {
	var parent [xvmroot.Size]byte
	us := benchUTXOSet()
	rng := rand.New(rand.NewSource(2))
	as := randAssetLeaves(rng, 4096)
	ts := randTxLeaves(rng, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = xvmroot.ExecutionRoot(parent, us, as, ts, 100)
	}
}

func BenchmarkExecutionRootParallel(b *testing.B) {
	var parent [xvmroot.Size]byte
	us := benchUTXOSet()
	rng := rand.New(rand.NewSource(2))
	as := randAssetLeaves(rng, 4096)
	ts := randTxLeaves(rng, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = xvmExecutionRoot(parent, us, as, ts, 100)
	}
}

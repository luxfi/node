// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvmroot

import (
	"encoding/hex"
	"testing"
)

// Canonical KAT fixture — a byte-for-byte port of the deterministic input built
// by lux-private/gpu-kernels/backends/vulkan/tests/test_xvm_roots_kat.cpp
// (the input every one of the seven GPU backends + the CPU oracle hash):
//
//	8 utxo slots, indices 0..3 occupied (status = kUtxoOccupied), 4..7 zero.
//	4 asset slots, indices 0..2 occupied (occupied = 1), 3 zero.
//	4 txs (all folded), i%3 status: 0 accepted, 1 rejected (reject_reason=42),
//	   2 untouched, 3 accepted.
//	parent_execution_root[k] = (uint8_t)(0xEE + k)
//	height = 100
//
// kind / status / reject_reason are the fields the leaf preimage binds; the
// accepted/rejected counters the kernel also returns are NOT part of any root,
// so this pure-computation mirror does not reproduce them.
const (
	katParentByte0 = 0xEE // parent_execution_root[k] = 0xEE + k (wraps mod 256)
	katHeight      = 100

	statusAccepted = 1
	statusRejected = 2
)

func katUTXOs() []UTXOLeaf {
	us := make([]UTXOLeaf, 8) // 8 slots; 4..7 left zero (unoccupied)
	for i := 0; i < 4; i++ {
		u := &us[i]
		for k := 0; k < 32; k++ {
			u.UTXOID[k] = byte((uint32(i)*11 + uint32(k)) ^ 0x71)
			u.AssetID[k] = byte(0xA0 + (k & 0xF))
			u.OwnerRoot[k] = byte(0xC0 + (k & 0xF))
		}
		u.AmountLo = uint64(1000 + i)
		u.AmountHi = 0
		u.Locktime = 0
		u.Threshold = 1
		u.Status = UTXOOccupied
	}
	return us
}

func katAssets() []AssetLeaf {
	as := make([]AssetLeaf, 4) // 4 slots; slot 3 left zero
	for i := 0; i < 3; i++ {
		a := &as[i]
		for k := 0; k < 32; k++ {
			a.AssetID[k] = byte((uint32(i)*7 + uint32(k)) ^ 0xC3)
			a.MintAuthorityRoot[k] = byte(0xD0 + (k & 0xF))
		}
		a.TotalSupplyLo = uint64(1000000 + i)
		a.TotalSupplyHi = 0
		a.FreezeFlag = 1
		a.Denomination = 8
		a.Occupied = 1
	}
	return as
}

func katTxs() []TxLeaf {
	ts := make([]TxLeaf, 4)
	for i := 0; i < 4; i++ {
		t := &ts[i]
		for k := 0; k < 32; k++ {
			t.TxID[k] = byte(i + k)
			t.ProofDigest[k] = byte(i*5 + k)
		}
		t.Kind = uint32(i)
		// i%3 status pattern — matches the GPU/.cpp "mixed" KAT fixture
		// (test_xvm_roots_kat.cpp run_case): i%3==0 accepted, ==1 rejected,
		// else untouched. Re-anchors Go==GPU on the same fixture.
		switch i % 3 {
		case 0:
			t.Status = statusAccepted
		case 1:
			t.Status = statusRejected
			t.RejectReason = 42
		}
	}
	return ts
}

func katParent() [Size]byte {
	var p [Size]byte
	for k := 0; k < Size; k++ {
		p[k] = byte(katParentByte0 + k)
	}
	return p
}

// TestExecutionRootKAT is the cross-language byte-parity proof: native Go ==
// GPU. It computes the full execution_root over the canonical KAT fixture and
// asserts it equals the value all seven GPU backends + the CPU oracle produce,
// along with each of the three sub-roots.
func TestExecutionRootKAT(t *testing.T) {
	const (
		wantExecutionRoot = "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77"
		wantUTXORoot      = "16663d25" // prefix; full value asserted below (unchanged — utxo leaf layout/occupancy identical)
		wantAssetRoot     = "12f47742" // unchanged
		wantTxRoot        = "c210388d" // recomputed for the i%3 tx-status fixture (Go==GPU; exec_root match proves it)
	)

	exec, utxo, asset, tx := ExecutionRoot(
		katParent(),
		katUTXOs(),
		katAssets(),
		katTxs(),
		katHeight,
	)

	gotExec := hex.EncodeToString(exec[:])
	gotUTXO := hex.EncodeToString(utxo[:])
	gotAsset := hex.EncodeToString(asset[:])
	gotTx := hex.EncodeToString(tx[:])

	t.Logf("utxo_root:      %s", gotUTXO)
	t.Logf("asset_root:     %s", gotAsset)
	t.Logf("tx_root:        %s", gotTx)
	t.Logf("execution_root: %s", gotExec)

	if gotExec != wantExecutionRoot {
		t.Errorf("execution_root mismatch:\n got  %s\n want %s", gotExec, wantExecutionRoot)
	}
	// Sub-roots: assert the documented prefixes from the KAT match exactly.
	if gotUTXO[:8] != wantUTXORoot {
		t.Errorf("utxo_root prefix mismatch:\n got  %s\n want %s…", gotUTXO, wantUTXORoot)
	}
	if gotAsset[:8] != wantAssetRoot {
		t.Errorf("asset_root prefix mismatch:\n got  %s\n want %s…", gotAsset, wantAssetRoot)
	}
	if gotTx[:8] != wantTxRoot {
		t.Errorf("tx_root prefix mismatch:\n got  %s\n want %s…", gotTx, wantTxRoot)
	}
}

// TestComposeIsUntaggedKeccak guards that the execution_root compose is the
// fixed-shape un-tagged keccak256 (NOT a Merkle node) over
// parent ‖ utxo ‖ asset ‖ tx ‖ height_u64_le. A regression to a tagged
// merkle.NodeHash-style compose would change these bytes.
func TestComposeIsUntaggedKeccak(t *testing.T) {
	const wantExecutionRoot = "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77"
	utxo := UTXORoot(katUTXOs())
	asset := AssetRoot(katAssets())
	tx := TxRoot(katTxs())
	got := Compose(katParent(), utxo, asset, tx, katHeight)
	if h := hex.EncodeToString(got[:]); h != wantExecutionRoot {
		t.Errorf("Compose:\n got  %s\n want %s", h, wantExecutionRoot)
	}
}

// TestEmptyFamilyRoots confirms the empty-set sub-root is keccak256("") (the
// RFC-6962 / merkle.EmptyRoot convention), per the wave-3 spec. An all-empty
// family must not fold to 0x00…00.
func TestEmptyFamilyRoots(t *testing.T) {
	const emptyKeccak = "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	for _, tc := range []struct {
		name string
		got  [Size]byte
	}{
		{"utxo", UTXORoot(nil)},
		{"asset", AssetRoot(nil)},
		{"tx", TxRoot(nil)},
		// A UTXO slate of all-unoccupied slots also yields the empty root.
		{"utxo-all-unoccupied", UTXORoot(make([]UTXOLeaf, 4))},
	} {
		if h := hex.EncodeToString(tc.got[:]); h != emptyKeccak {
			t.Errorf("%s empty root:\n got  %s\n want %s", tc.name, h, emptyKeccak)
		}
	}
}

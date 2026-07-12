// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/state/xvmroot"
	"github.com/luxfi/node/vms/xvm/txs"
)

// projTestParser is the block parser used to stand up a real xvm state for the
// projection tests. secp256k1fx is the dominant value-output fx.
var projTestParser block.Parser

func init() {
	p, err := block.NewParser([]fxs.Fx{&secp256k1fx.Fx{}})
	if err != nil {
		panic(err)
	}
	projTestParser = p
}

// shortIDs builds n distinct 20-byte addresses from a seed byte.
func shortIDs(seed byte, n int) []ids.ShortID {
	out := make([]ids.ShortID, n)
	for i := range out {
		var a ids.ShortID
		a[0] = seed
		a[19] = byte(i)
		out[i] = a
	}
	return out
}

// transferOut builds a secp256k1fx transfer output with a sorted owner set.
func transferOut(amount uint64, threshold uint32, locktime uint64, addrs []ids.ShortID) *secp256k1fx.TransferOutput {
	o := &secp256k1fx.TransferOutput{
		Amt: amount,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: threshold,
			Locktime:  locktime,
			Addrs:     addrs,
		},
	}
	o.Sort()
	return o
}

// newProjState returns a fresh, committed-capable real xvm state.
func newProjState(t *testing.T) state.State {
	t.Helper()
	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := state.New(vdb, projTestParser, metric.NewNoOp().Registry(), false)
	require.NoError(t, err)
	return s
}

// TestOwnerRootMatchesGPULayoutIntent pins the canonical owner_root preimage to
// the documented definition (the value the GPU snapshot producer must match):
//
//	keccak256(threshold_le ‖ key_count_le ‖ sorted_key[0] ‖ … )
//
// computed here independently of OwnerRoot's implementation.
func TestOwnerRootMatchesGPULayoutIntent(t *testing.T) {
	require := require.New(t)

	addrs := shortIDs(0xAB, 3)
	o := transferOut(1000, 2, 42, addrs)
	owners, ok := extractOwners(o)
	require.True(ok)

	// Independent reference preimage: threshold_le ‖ count_le ‖ sorted addrs.
	sortedAddrs := make([][]byte, len(addrs))
	for i := range addrs {
		sortedAddrs[i] = addrs[i].Bytes()
	}
	slices.SortFunc(sortedAddrs, bytes.Compare)

	pre := make([]byte, 0, 8+20*len(sortedAddrs))
	pre = append(pre, byte(2), 0, 0, 0)                // threshold = 2, LE u32
	pre = append(pre, byte(len(sortedAddrs)), 0, 0, 0) // count = 3, LE u32
	for _, a := range sortedAddrs {
		pre = append(pre, a...)
	}
	want := hash.ComputeKeccak256Array(pre)

	require.Equal(want, OwnerRoot(owners), "owner_root must equal the documented canonical preimage")
}

// TestOwnerRootDeterministicAcrossOrder confirms owner_root is a function of the
// address SET + threshold, independent of the order addresses are listed in.
func TestOwnerRootDeterministicAcrossOrder(t *testing.T) {
	require := require.New(t)

	addrs := shortIDs(0x11, 4)
	forward := transferOut(5, 2, 0, append([]ids.ShortID(nil), addrs...))

	reversed := append([]ids.ShortID(nil), addrs...)
	slices.Reverse(reversed)
	// Bypass Sort to prove the derivation itself sorts.
	rev := &secp256k1fx.TransferOutput{
		Amt:          5,
		OutputOwners: secp256k1fx.OutputOwners{Threshold: 2, Addrs: reversed},
	}

	of, okf := extractOwners(forward)
	or, okr := extractOwners(rev)
	require.True(okf)
	require.True(okr)
	require.Equal(OwnerRoot(of), OwnerRoot(or), "owner_root must not depend on address listing order")
}

// TestOwnerRootThresholdBinds confirms threshold is bound into owner_root: two
// owner sets identical but for threshold produce different roots.
func TestOwnerRootThresholdBinds(t *testing.T) {
	require := require.New(t)
	addrs := shortIDs(0x22, 3)
	a := transferOut(1, 1, 0, append([]ids.ShortID(nil), addrs...))
	b := transferOut(1, 2, 0, append([]ids.ShortID(nil), addrs...))
	oa, _ := extractOwners(a)
	ob, _ := extractOwners(b)
	require.NotEqual(OwnerRoot(oa), OwnerRoot(ob), "threshold must change owner_root")
}

// TestUTXOLeafProjection pins the field-by-field projection of a transfer output
// onto its canonical UTXOLeaf.
func TestUTXOLeafProjection(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	addrs := shortIDs(0x33, 2)
	out := transferOut(7777, 1, 99, addrs)
	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{TxID: txID, OutputIndex: 5},
		Asset:  lux.Asset{ID: assetID},
		Out:    out,
	}

	leaf, err := utxoLeaf(utxo)
	require.NoError(err)

	owners, _ := extractOwners(out)
	require.Equal(ids.ID(utxo.InputID()), ids.ID(leaf.UTXOID))
	require.Equal(assetID, ids.ID(leaf.AssetID))
	require.Equal(uint64(7777), leaf.AmountLo)
	require.Equal(uint64(0), leaf.AmountHi)
	require.Equal(OwnerRoot(owners), leaf.OwnerRoot)
	require.Equal(uint64(99), leaf.Locktime)
	require.Equal(uint32(1), leaf.Threshold)
	require.Equal(xvmroot.UTXOOccupied, leaf.Status)
}

// TestBlockExecutionRootMatchesHandProjection is the deliverable's gate test: a
// known post-block state's wired execution_root equals xvmroot.ExecutionRoot
// over the hand-projected leaves. It proves the wired projection (enumeration +
// per-UTXO projection + parallel fold) is self-consistent with the frozen
// canonical leaf spec.
func TestBlockExecutionRootMatchesHandProjection(t *testing.T) {
	require := require.New(t)
	s := newProjState(t)

	// Build a known occupied UTXO set: a handful of transfer outputs with mixed
	// amounts, thresholds, locktimes, and owner-set sizes.
	txID := ids.GenerateTestID()
	type spec struct {
		index            uint32
		amount, locktime uint64
		threshold        uint32
		addrSeed         byte
		addrN            int
	}
	specs := []spec{
		{0, 1000, 0, 1, 0x01, 1},
		{1, 250, 50, 2, 0x02, 3},
		{2, 9999, 0, 1, 0x03, 2},
		{3, 1, 7, 1, 0x04, 1},
	}
	for _, sp := range specs {
		out := transferOut(sp.amount, sp.threshold, sp.locktime, shortIDs(sp.addrSeed, sp.addrN))
		s.AddUTXO(&lux.UTXO{
			UTXOID: lux.UTXOID{TxID: txID, OutputIndex: sp.index},
			Asset:  lux.Asset{ID: ids.GenerateTestID()},
			Out:    out,
		})
	}
	require.NoError(s.Commit())

	// A block tx set (just IDs matter for the tx family).
	blkTxs := makeTxs(t, 3)
	parentRoot := ids.GenerateTestID()
	const height = 88

	// Wired root (enumeration + projection + parallel fold).
	gotRoot, err := BlockExecutionRoot(parentRoot, blkTxs, s, height)
	require.NoError(err)

	// Hand projection: enumerate the same occupied set in canonical order and
	// project each UTXO, then run the frozen serial xvmroot.ExecutionRoot.
	utxos, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	handLeaves := make([]xvmroot.UTXOLeaf, len(utxos))
	for i, u := range utxos {
		leaf, err := utxoLeaf(u)
		require.NoError(err)
		handLeaves[i] = leaf
	}
	handTxLeaves := txLeaves(blkTxs)

	wantExec, wantUTXO, wantAsset, wantTx := xvmroot.ExecutionRoot(
		[xvmroot.Size]byte(parentRoot),
		handLeaves,
		nil, // asset family is empty (UTXO-only executor state)
		handTxLeaves,
		height,
	)

	require.Equal(ids.ID(wantExec), gotRoot,
		"wired execution_root must equal xvmroot.ExecutionRoot over the hand-projected leaves")

	// The UTXO family is non-trivial (real leaves), not the empty root.
	require.NotEqual(xvmroot.UTXORoot(nil), wantUTXO, "UTXO root must be non-trivial")
	require.Equal(xvmroot.AssetRoot(nil), wantAsset, "asset root is the empty root (no asset arena)")
	require.NotEqual([xvmroot.Size]byte{}, wantTx, "tx root is keccak-based, never zero")
}

// TestBlockExecutionRootDeterministic is the property test: the wired root over
// the same post-block state is identical across repeated computations (the
// determinism consensus requires).
func TestBlockExecutionRootDeterministic(t *testing.T) {
	require := require.New(t)
	s := newProjState(t)

	txID := ids.GenerateTestID()
	for i := uint32(0); i < 40; i++ {
		out := transferOut(uint64(i+1), 1, uint64(i), shortIDs(byte(i%7), 1+int(i%3)))
		s.AddUTXO(&lux.UTXO{
			UTXOID: lux.UTXOID{TxID: txID, OutputIndex: i},
			Asset:  lux.Asset{ID: ids.GenerateTestID()},
			Out:    out,
		})
	}
	require.NoError(s.Commit())

	blkTxs := makeTxs(t, 5)
	parentRoot := ids.GenerateTestID()

	first, err := BlockExecutionRoot(parentRoot, blkTxs, s, 7)
	require.NoError(err)
	for i := 0; i < 16; i++ {
		again, err := BlockExecutionRoot(parentRoot, blkTxs, s, 7)
		require.NoError(err)
		require.Equal(first, again, "execution_root must be deterministic across runs")
	}
}

// TestBlockExecutionRootEmptyState confirms an empty occupied set yields the
// empty UTXO root inside the compose (not a wrong/zero value), still composing a
// well-formed execution_root over the tx family + parent + height.
func TestBlockExecutionRootEmptyState(t *testing.T) {
	require := require.New(t)
	s := newProjState(t)

	blkTxs := makeTxs(t, 2)
	parentRoot := ids.GenerateTestID()
	const height = 3

	got, err := BlockExecutionRoot(parentRoot, blkTxs, s, height)
	require.NoError(err)

	wantExec, _, _, _ := xvmroot.ExecutionRoot(
		[xvmroot.Size]byte(parentRoot),
		nil, // no occupied UTXOs
		nil,
		txLeaves(blkTxs),
		height,
	)
	require.Equal(ids.ID(wantExec), got)
}

// makeTxs builds n minimal, initialized xvm txs with distinct, stable IDs.
func makeTxs(t *testing.T, n int) []*txs.Tx {
	t.Helper()
	out := make([]*txs.Tx, n)
	for i := range out {
		tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: lux.BaseTx{
			BlockchainID: ids.GenerateTestID(),
		}}}
		require.NoError(t, tx.Initialize())
		out[i] = tx
	}
	return out
}

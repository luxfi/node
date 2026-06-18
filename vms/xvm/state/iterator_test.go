// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	lux "github.com/luxfi/utxo"

	"github.com/luxfi/utxo/secp256k1fx"
)

// utxoWith builds a deterministic secp256k1fx UTXO with the given txID/index and
// amount. The (txID, index) pair determines its UTXOID (InputID).
func utxoWith(txID ids.ID, index uint32, amount uint64) *lux.UTXO {
	return &lux.UTXO{
		UTXOID: lux.UTXOID{TxID: txID, OutputIndex: index},
		Asset:  lux.Asset{ID: ids.GenerateTestID()},
		Out:    &secp256k1fx.TransferOutput{Amt: amount},
	}
}

// idsOf returns the UTXOIDs of a UTXO slice, for order/content assertions.
func idsOf(utxos []*lux.UTXO) []ids.ID {
	out := make([]ids.ID, len(utxos))
	for i, u := range utxos {
		out[i] = u.InputID()
	}
	return out
}

// sortedIDsOf returns the ascending-sorted UTXOIDs of a UTXO slice.
func sortedIDsOf(utxos []*lux.UTXO) []ids.ID {
	out := idsOf(utxos)
	ids.Sort(out)
	return out
}

func newTestState(t *testing.T) (State, *versiondb.Database) {
	t.Helper()
	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := New(vdb, parser, metric.NewNoOp().Registry(), trackChecksums)
	require.NoError(t, err)
	return s, vdb
}

// TestUTXOsAscendingOrder confirms committed UTXOs enumerate in strictly
// ascending UTXOID order, regardless of insertion order.
func TestUTXOsAscendingOrder(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	want := make([]*lux.UTXO, 0, 16)
	for i := uint32(0); i < 16; i++ {
		u := utxoWith(txID, i, uint64(100+i))
		want = append(want, u)
		s.AddUTXO(u)
	}
	require.NoError(s.Commit())

	got, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.Len(got, len(want))

	// Strictly ascending and equal to the sorted want set.
	require.Equal(sortedIDsOf(want), idsOf(got))
	require.True(slices.IsSortedFunc(idsOf(got), func(a, b ids.ID) int { return a.Compare(b) }))
}

// TestUTXOsModifiedOverlay confirms the in-memory modifiedUTXOs overlay wins
// over committed records: an added UTXO appears, a deleted UTXO disappears, and
// the merged stream stays in ascending order.
func TestUTXOsModifiedOverlay(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	// Commit four UTXOs at indices 0..3.
	committed := make([]*lux.UTXO, 4)
	for i := uint32(0); i < 4; i++ {
		committed[i] = utxoWith(txID, i, uint64(10+i))
		s.AddUTXO(committed[i])
	}
	require.NoError(s.Commit())

	// Uncommitted: add two new UTXOs and delete one committed UTXO.
	added := []*lux.UTXO{utxoWith(txID, 100, 999), utxoWith(txID, 101, 1000)}
	for _, u := range added {
		s.AddUTXO(u)
	}
	s.DeleteUTXO(committed[1].InputID())

	got, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)

	want := []*lux.UTXO{committed[0], committed[2], committed[3], added[0], added[1]}
	require.ElementsMatch(idsOf(want), idsOf(got))
	require.NotContains(idsOf(got), committed[1].InputID(), "deleted UTXO must not appear")
	require.True(slices.IsSortedFunc(idsOf(got), func(a, b ids.ID) int { return a.Compare(b) }),
		"merged stream must stay ascending")
}

// TestUTXOsModifiedReplace confirms an added UTXO whose UTXOID equals a committed
// one shadows the committed value (replacement, not duplication).
func TestUTXOsModifiedReplace(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	orig := utxoWith(txID, 7, 500)
	s.AddUTXO(orig)
	require.NoError(s.Commit())

	// Re-add the same UTXOID with a different amount (uncommitted).
	replacement := utxoWith(txID, 7, 777)
	require.Equal(orig.InputID(), replacement.InputID())
	s.AddUTXO(replacement)

	got, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.Len(got, 1, "the same UTXOID must appear exactly once")
	require.Equal(uint64(777), got[0].Out.(*secp256k1fx.TransferOutput).Amt,
		"overlay value must shadow the committed one")
}

// TestUTXOsStartAndLimit confirms the (start, limit) paging contract: start is
// exclusive, limit bounds the page, and paging by the last UTXOID covers the
// whole set exactly once in order.
func TestUTXOsStartAndLimit(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	all := make([]*lux.UTXO, 20)
	for i := uint32(0); i < 20; i++ {
		all[i] = utxoWith(txID, i, uint64(i))
		s.AddUTXO(all[i])
	}
	require.NoError(s.Commit())

	wantOrder := sortedIDsOf(all)

	// limit bounds the page.
	page, err := s.UTXOs(ids.Empty, 5)
	require.NoError(err)
	require.Equal(wantOrder[:5], idsOf(page))

	// start is exclusive: paging from the last of the first page yields the next.
	page2, err := s.UTXOs(wantOrder[4], 5)
	require.NoError(err)
	require.Equal(wantOrder[5:10], idsOf(page2))

	// Full page-by-page walk reconstructs the whole set in order, exactly once.
	var walked []ids.ID
	start := ids.Empty
	for {
		p, err := s.UTXOs(start, 7)
		require.NoError(err)
		if len(p) == 0 {
			break
		}
		walked = append(walked, idsOf(p)...)
		start = p[len(p)-1].InputID()
	}
	require.Equal(wantOrder, walked)
}

// TestUTXOsEmpty confirms an empty chain enumerates to no UTXOs.
func TestUTXOsEmpty(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	got, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.Empty(got)
}

// TestDiffUTXOsOverlaysParent confirms a diff enumerates the parent's occupied
// set with the diff's own adds/deletes overlaid, in ascending order — the
// post-block enumeration the execution_root projection consumes.
func TestDiffUTXOsOverlaysParent(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	parentUTXOs := make([]*lux.UTXO, 6)
	for i := uint32(0); i < 6; i++ {
		parentUTXOs[i] = utxoWith(txID, i, uint64(i))
		s.AddUTXO(parentUTXOs[i])
	}
	require.NoError(s.Commit())

	parentID := ids.GenerateTestID()
	d, err := NewDiff(parentID, &versions{chains: map[ids.ID]Chain{parentID: s}})
	require.NoError(err)

	// In the diff: add one new UTXO, delete two parent UTXOs.
	newUTXO := utxoWith(txID, 200, 12345)
	d.AddUTXO(newUTXO)
	d.DeleteUTXO(parentUTXOs[0].InputID())
	d.DeleteUTXO(parentUTXOs[3].InputID())

	got, err := d.UTXOs(ids.Empty, 0)
	require.NoError(err)

	want := []*lux.UTXO{parentUTXOs[1], parentUTXOs[2], parentUTXOs[4], parentUTXOs[5], newUTXO}
	require.ElementsMatch(idsOf(want), idsOf(got))
	require.NotContains(idsOf(got), parentUTXOs[0].InputID())
	require.NotContains(idsOf(got), parentUTXOs[3].InputID())
	require.True(slices.IsSortedFunc(idsOf(got), func(a, b ids.ID) int { return a.Compare(b) }))
}

// TestDiffUTXOsPagesLargeParent confirms the diff's parent enumeration pages
// correctly past parentPageSize: a parent with more than one page of UTXOs is
// fully streamed, in order, with no gaps or repeats at the page boundary.
func TestDiffUTXOsPagesLargeParent(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	// Two-and-a-bit pages worth, to straddle the parentPageSize boundary.
	n := 2*parentPageSize + 37
	txID := ids.GenerateTestID()
	parentUTXOs := make([]*lux.UTXO, n)
	for i := 0; i < n; i++ {
		parentUTXOs[i] = utxoWith(txID, uint32(i), uint64(i))
		s.AddUTXO(parentUTXOs[i])
	}
	require.NoError(s.Commit())

	parentID := ids.GenerateTestID()
	d, err := NewDiff(parentID, &versions{chains: map[ids.ID]Chain{parentID: s}})
	require.NoError(err)

	got, err := d.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.Len(got, n)
	require.Equal(sortedIDsOf(parentUTXOs), idsOf(got), "all parent UTXOs in ascending order across page boundaries")
}

// TestUTXOsTrailingAndPhantomRemovals confirms the overlay merge handles
// removals that sort after every committed record (trailing) and removals of
// UTXOIDs that were never committed (phantom): both emit nothing and never
// corrupt the stream.
func TestUTXOsTrailingAndPhantomRemovals(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	committed := make([]*lux.UTXO, 3)
	for i := uint32(0); i < 3; i++ {
		committed[i] = utxoWith(txID, i, uint64(i))
		s.AddUTXO(committed[i])
	}
	require.NoError(s.Commit())

	// A removal of a high-index UTXO that was never committed: it has a UTXOID
	// that (by the txID.Prefix scheme) may sort before or after the committed
	// ones; either way it must not appear and must not drop a real record.
	s.DeleteUTXO(utxoWith(txID, 9999, 0).InputID())
	// A removal of a phantom UTXO from a different tx entirely.
	s.DeleteUTXO(utxoWith(ids.GenerateTestID(), 0, 0).InputID())

	got, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.Equal(sortedIDsOf(committed), idsOf(got),
		"phantom/trailing removals must leave the committed set intact and ordered")
}

// TestUTXOsDeterministic confirms repeated enumeration of the same state yields
// byte-identical ordering — the determinism the consensus root depends on.
func TestUTXOsDeterministic(t *testing.T) {
	require := require.New(t)
	s, _ := newTestState(t)

	txID := ids.GenerateTestID()
	for i := uint32(0); i < 50; i++ {
		s.AddUTXO(utxoWith(txID, i, uint64(i)))
	}
	require.NoError(s.Commit())

	first, err := s.UTXOs(ids.Empty, 0)
	require.NoError(err)
	for i := 0; i < 8; i++ {
		again, err := s.UTXOs(ids.Empty, 0)
		require.NoError(err)
		require.Equal(idsOf(first), idsOf(again), "enumeration order must be deterministic across runs")
	}
}

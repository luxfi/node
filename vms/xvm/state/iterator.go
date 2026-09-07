// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
)

// UTXOs enumerates the chain's occupied UTXO set in canonical ascending-UTXOID
// order — the deterministic full-set ordering the xvm execution_root projection
// folds. It merges the committed UTXO records (read from utxoRecordDB, already
// in ascending-UTXOID order because the database iterates keys in order and the
// key is the raw UTXOID) with the in-memory modifiedUTXOs overlay (this state's
// uncommitted adds and removals). The overlay wins: an added UTXO replaces or
// inserts its record; a removed UTXO (nil in the map) is excluded.
func (s *state) UTXOs(start ids.ID, limit int) ([]*lux.UTXO, error) {
	iter := s.utxoRecordDB.NewIteratorWithStart(start[:])
	defer iter.Release()
	return mergeUTXOOverlay(committedUTXOStream(iter, s.parseRecord), s.modifiedUTXOs, start, limit)
}

// UTXOs enumerates the diff's occupied UTXO set in canonical ascending-UTXOID
// order: the parent chain's occupied set merged with this diff's modifiedUTXOs
// overlay (the block's applied adds and removals). The parent is enumerated
// through the same UTXOs contract, so the composition is uniform — a diff over a
// diff over committed state all resolve to one ascending-UTXOID stream.
func (d *diff) UTXOs(start ids.ID, limit int) ([]*lux.UTXO, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return mergeUTXOOverlay(parentUTXOStream(parentState), d.modifiedUTXOs, start, limit)
}

// utxoStream is a cursor over an ascending-UTXOID UTXO source. next reports
// whether a UTXO is available; cur returns the current (id, utxo). It abstracts
// the committed-record iterator and a parent ReadOnlyChain behind one shape so
// mergeUTXOOverlay is written once.
type utxoStream interface {
	// next advances to the UTXO with the smallest UTXOID strictly greater than
	// the cursor's prior position (or the start bound on the first call) and
	// reports whether one exists.
	next() (bool, error)
	// cur returns the UTXO the cursor currently rests on. Only valid after next
	// returned (true, nil).
	cur() (ids.ID, *lux.UTXO)
}

// parseRecord decodes a committed UTXO record's wire bytes.
func (s *state) parseRecord(b []byte) (*lux.UTXO, error) {
	return lux.ParseUTXO(b)
}

// committedRecordStream is a utxoStream over the raw committed UTXO record
// database iterator (keys are UTXOIDs in ascending order, values are wire
// bytes).
type committedRecordStream struct {
	iter  database.Iterator
	parse func([]byte) (*lux.UTXO, error)
	id    ids.ID
	utxo  *lux.UTXO
}

func committedUTXOStream(iter database.Iterator, parse func([]byte) (*lux.UTXO, error)) *committedRecordStream {
	return &committedRecordStream{iter: iter, parse: parse}
}

func (c *committedRecordStream) next() (bool, error) {
	if !c.iter.Next() {
		return false, c.iter.Error()
	}
	id, err := ids.ToID(c.iter.Key())
	if err != nil {
		return false, err
	}
	utxo, err := c.parse(c.iter.Value())
	if err != nil {
		return false, err
	}
	c.id, c.utxo = id, utxo
	return true, nil
}

func (c *committedRecordStream) cur() (ids.ID, *lux.UTXO) { return c.id, c.utxo }

// parentRecordStream is a utxoStream that pages a parent ReadOnlyChain through
// its UTXOs contract. It fetches a bounded page at a time and refills from the
// last UTXOID seen, so an arbitrarily large parent set streams without loading
// the whole set into memory at once.
type parentRecordStream struct {
	parent ReadOnlyChain
	page   []*lux.UTXO
	idx    int
	last   ids.ID
	primed bool
	done   bool
	id     ids.ID
	utxo   *lux.UTXO
}

// parentPageSize bounds each refill from the parent. It trades fewer round
// trips (larger) against transient memory (smaller); 1024 mirrors the per-page
// scale the rest of the UTXO layer uses.
const parentPageSize = 1024

func parentUTXOStream(parent ReadOnlyChain) *parentRecordStream {
	return &parentRecordStream{parent: parent, last: ids.Empty}
}

func (p *parentRecordStream) next() (bool, error) {
	for {
		if p.idx < len(p.page) {
			u := p.page[p.idx]
			p.idx++
			p.id, p.utxo = u.InputID(), u
			p.last = p.id
			return true, nil
		}
		if p.done {
			return false, nil
		}
		// The first refill starts at ids.Empty (the beginning); subsequent
		// refills start strictly after the last UTXOID returned.
		page, err := p.parent.UTXOs(p.last, parentPageSize)
		if err != nil {
			return false, err
		}
		p.primed = true
		if len(page) < parentPageSize {
			// A short (or empty) page means the parent is exhausted after this
			// page; no further refill can yield more.
			p.done = true
		}
		if len(page) == 0 {
			return false, nil
		}
		p.page, p.idx = page, 0
	}
}

func (p *parentRecordStream) cur() (ids.ID, *lux.UTXO) { return p.id, p.utxo }

// mergeUTXOOverlay folds an ascending-UTXOID base stream against an in-memory
// modification overlay into the occupied UTXO set, in strictly increasing
// UTXOID order, beginning after [start] and bounded by [limit] (<= 0 means
// unbounded). The overlay is authoritative: a base record whose UTXOID is also
// in the overlay is shadowed by the overlay value (a non-nil entry replaces it;
// a nil entry — a removal — drops it). Overlay entries with no base record are
// inserts. The merge is a classic ordered set-merge: the base stream is already
// ordered, and the overlay's own UTXOIDs are sorted once so both sides advance
// monotonically.
//
// This single function serves both *state (committed records + uncommitted
// modifiedUTXOs) and *diff (parent stream + the diff's modifiedUTXOs), so the
// occupied-set semantics live in exactly one place.
func mergeUTXOOverlay(
	base utxoStream,
	overlay map[ids.ID]*lux.UTXO,
	start ids.ID,
	limit int,
) ([]*lux.UTXO, error) {
	overlayIDs := sortedOverlayIDsAfter(overlay, start)
	oi := 0

	var out []*lux.UTXO
	appendUTXO := func(u *lux.UTXO) bool {
		out = append(out, u)
		return limit <= 0 || len(out) < limit
	}

	hasBase, err := base.next()
	if err != nil {
		return nil, err
	}
	for hasBase {
		baseID, baseUTXO := base.cur()
		if baseID.Compare(start) <= 0 {
			// Skip anything at or before the exclusive start bound. The
			// committed iterator already starts at [start], but its first key
			// may equal [start] (NewIteratorWithStart is inclusive), and the
			// parent stream is unfiltered, so enforce the exclusive bound here.
			hasBase, err = base.next()
			if err != nil {
				return nil, err
			}
			continue
		}

		// Drain overlay entries that sort before this base record (pure inserts).
		for oi < len(overlayIDs) && overlayIDs[oi].Compare(baseID) < 0 {
			id := overlayIDs[oi]
			oi++
			if u := overlay[id]; u != nil {
				if !appendUTXO(u) {
					return out, nil
				}
			}
		}

		if oi < len(overlayIDs) && overlayIDs[oi] == baseID {
			// Overlay shadows this base record: emit the overlay value unless
			// it is a removal (nil), and consume both sides.
			id := overlayIDs[oi]
			oi++
			if u := overlay[id]; u != nil {
				if !appendUTXO(u) {
					return out, nil
				}
			}
			hasBase, err = base.next()
			if err != nil {
				return nil, err
			}
			continue
		}

		// No overlay entry for this base record: emit it as-is.
		if !appendUTXO(baseUTXO) {
			return out, nil
		}
		hasBase, err = base.next()
		if err != nil {
			return nil, err
		}
	}

	// Base exhausted: drain remaining overlay inserts (still in ascending order).
	for oi < len(overlayIDs) {
		id := overlayIDs[oi]
		oi++
		if u := overlay[id]; u != nil {
			if !appendUTXO(u) {
				return out, nil
			}
		}
	}
	return out, nil
}

// sortedOverlayIDsAfter returns the overlay's UTXOIDs strictly greater than
// [start], in ascending order — the order mergeUTXOOverlay consumes them in. It
// includes removal entries (nil values): a removal at an ID must still be
// matched against a base record so the base record is dropped, even though the
// removal itself emits nothing.
func sortedOverlayIDsAfter(overlay map[ids.ID]*lux.UTXO, start ids.ID) []ids.ID {
	out := make([]ids.ID, 0, len(overlay))
	for id := range overlay {
		if id.Compare(start) > 0 {
			out = append(out, id)
		}
	}
	ids.Sort(out)
	return out
}

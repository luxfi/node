// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// ChainEntry is the fixed-stride wire entry of a ChainsList. Each entry
// pins one chain's (Name, VMID, FxIDs, GenesisData) tuple. Variable-length
// content (Name bytes, FxIDs blob, GenesisData bytes) lives in three
// sibling arrays carried as parent-tx fields; the entry stores
// (relativeOffset, length) cursors into them.
//
// This mirrors the BoundEvidenceList pattern (batch 3) for orthogonal
// composability: a fixed-stride list payload + sibling blob arrays +
// type-level Bind to attach the blobs and unlock safe accessors.
//
// Wire layout per ChainEntry (stride 64 bytes):
//
//	NameRel          uint32 @ 0        (byte offset within NameBlobs)
//	NameLen          uint32 @ 4
//	VMID             32B    @ 8        (the VM identifier)
//	FxIDsRel         uint32 @ 40       (byte offset within FxIDsBlobs)
//	FxIDsLen         uint32 @ 44       (in BYTES; entry count = Len / 32)
//	GenesisDataRel   uint32 @ 48       (byte offset within GenesisDataBlobs)
//	GenesisDataLen   uint32 @ 52
//	Reserved         8B     @ 56..64   (zero — future expansion: subnet flags,
//	                                    initial supply hint, etc.)
//
// Stride = 64 bytes (8-byte aligned for cleanly composable List.Object reads).
const (
	OffsetChainEntry_NameRel        = 0
	OffsetChainEntry_NameLen        = 4
	OffsetChainEntry_VMID           = 8
	OffsetChainEntry_FxIDsRel       = 40
	OffsetChainEntry_FxIDsLen       = 44
	OffsetChainEntry_GenesisDataRel = 48
	OffsetChainEntry_GenesisDataLen = 52
	OffsetChainEntry_Reserved       = 56 // [56..64) — must be zero, gated by ChainsListView.MustVerify (R6-4 + R7V8)
	SizeChainEntry                  = 64

	// FxIDSize is the wire size of one chain ID inside the FxIDs blob.
	// FxIDsLen is in BYTES; entry count = FxIDsLen / FxIDSize.
	FxIDSize = 32
)

// ChainEntry is the zero-copy WIRE view over one entry in a ChainsListView.
// Like EvidenceEntry, it exposes raw cursor accessors only — the safe
// Name/FxIDs/GenesisData accessors live on BoundChainEntry, reachable
// only via ChainsListView.Bind(...). Compile-time enforcement of the
// "Bind-before-use" contract.
//
// ZERO-VALUE SAFETY: `ChainEntry{}` (returned by ChainsListView.At() on
// out-of-range / clamped lists) is detectable via IsNull(); calling any
// accessor on a zero-value entry returns the zero value (VMID=0,
// (0,0) cursors) instead of panicking. The IsNull guard is the
// defense-in-depth layer that pairs with the defensive At(i).
type ChainEntry struct {
	obj zap.Object
}

// IsNull returns true if the entry is the zero value (out-of-range
// At(i) result, or empty list). Accessors on a zero-value entry return
// zero rather than panic — callers may still want to short-circuit
// downstream work on IsNull entries.
func (e ChainEntry) IsNull() bool { return e.obj.IsNull() }

// VMID returns the chain's VM identifier. This is fixed-size and safe to
// read without binding. Returns zero-value on a zero entry.
func (e ChainEntry) VMID() ids.ID {
	if e.obj.IsNull() {
		return ids.ID{}
	}
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = e.obj.Uint8(OffsetChainEntry_VMID + i)
	}
	return out
}

// NameRange returns the raw (relativeOffset, length) wire cursors into
// the parent tx's NameBlobs array for this chain's name.
//
// UNSAFE — call Name() on BoundChainEntry instead. The raw (Rel, Len)
// pair carries no clamp; consumers indexing nb[rel:rel+len] panic if
// an adversary sets len=0xFFFFFFFF or rel > len(nb). Same surface as
// EvidenceEntry.MessageARange (RED-HIGH-3).
//
// Returns (0,0) on a zero-value entry.
func (e ChainEntry) NameRange() (uint32, uint32) {
	if e.obj.IsNull() {
		return 0, 0
	}
	return e.obj.Uint32(OffsetChainEntry_NameRel),
		e.obj.Uint32(OffsetChainEntry_NameLen)
}

// FxIDsRange returns the raw (relativeOffset, length-in-bytes) cursors
// into the parent tx's FxIDsBlobs array. Entry count = Len / FxIDSize.
//
// UNSAFE — call FxIDs() on BoundChainEntry instead.
// Returns (0,0) on a zero-value entry.
func (e ChainEntry) FxIDsRange() (uint32, uint32) {
	if e.obj.IsNull() {
		return 0, 0
	}
	return e.obj.Uint32(OffsetChainEntry_FxIDsRel),
		e.obj.Uint32(OffsetChainEntry_FxIDsLen)
}

// GenesisDataRange returns the raw (relativeOffset, length) cursors into
// the parent tx's GenesisDataBlobs array.
//
// UNSAFE — call GenesisData() on BoundChainEntry instead.
// Returns (0,0) on a zero-value entry.
func (e ChainEntry) GenesisDataRange() (uint32, uint32) {
	if e.obj.IsNull() {
		return 0, 0
	}
	return e.obj.Uint32(OffsetChainEntry_GenesisDataRel),
		e.obj.Uint32(OffsetChainEntry_GenesisDataLen)
}

// ChainsListView is the zero-copy WIRE view over a list of ChainEntry
// items. UNBOUND — raw cursor accessors work; safe Name/FxIDs/GenesisData
// accessors require .Bind(nameBlobs, fxIDsBlobs, genesisDataBlobs).
//
// Compile-time enforcement: ChainsListView lacks the safe accessors so
// consumers cannot bypass Bind() by accident.
//
// CROSS-BLOB ALIASING ALLOWANCE (LP-023 Red round 6 R6-2 design decision):
//
// A wire-encoded ChainsList may legitimately encode overlapping
// `(rel, len)` ranges across entries pointing into the shared
// NameBlobs / FxIDsBlobs / GenesisDataBlobs sibling arrays. For example,
// two chains with the same VMID may share the same FxIDsBlob range; two
// chains that share a name template can share the same NameBlob range.
// The writer (WriteChainsList) concatenates without dedup, so today
// this is a "won't happen by accident" path — but the WIRE layer must
// not reject it, because a future writer optimization may emit aliased
// ranges to shrink the blob arrays.
//
// CONTRACT (the wire layer makes these promises; consumers must respect them):
//
//  1. Name(), FxIDs(), GenesisData() return PAYLOAD slices into the
//     shared blob, NOT identity. Two distinct entries whose `(rel, len)`
//     windows overlap will return byte-equal (or byte-overlapping) slices.
//
//  2. Chain IDENTITY is the (VMID, BlockchainID) pair set by the
//     executor via CreateChainTx. Wire-layer Name is descriptive metadata,
//     not consensus-binding. The executor never derives identity from
//     Name() bytes.
//
//  3. Consumers MUST NOT use returned bytes as a deduplication key.
//     Two entries returning the same Name() bytes are two distinct chains
//     on the network if their (VMID, BlockchainID) differ. Using Name()
//     as a set membership key would silently merge them.
//
//  4. Returned slices are READ-ONLY. Mutating any byte returned from
//     Name() / FxIDs() / GenesisData() corrupts the parent message AND
//     any aliased entries simultaneously. The slice headers point into
//     the wire buffer; there is no copy boundary.
//
// If a future feature requires per-entry NON-overlapping ranges (e.g.
// to enable in-place mutation of one chain's name without affecting
// others), add a `ChainsListView.VerifyNonOverlappingRanges() error`
// method and call it from the relevant tx's Verify() entry. Until then,
// aliasing is allowed and exercised by the
// TestChainsList_AllowsOverlappingRanges_DocumentedContract test which
// pins the byte-equality guarantee.
type ChainsListView struct {
	list zap.List
}

// Len returns the chain count.
func (l ChainsListView) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l ChainsListView) IsNull() bool { return l.list.IsNull() }

// At returns the i'th ChainEntry in raw (unbound) form. Returns zero-value
// ChainEntry when out of range (defensive: a clamped ListStride view may
// have Len()=0 after R4V9 rejected a poisoned wire length).
func (l ChainsListView) At(i int) ChainEntry {
	if i < 0 || i >= l.list.Len() {
		return ChainEntry{}
	}
	return ChainEntry{obj: l.list.Object(i, SizeChainEntry)}
}

// Bind attaches the parent tx's NameBlobs, FxIDsBlobs, and GenesisDataBlobs
// and returns a BoundChainsList whose At() exposes safe Name/FxIDs/GenesisData
// accessors. The raw (Rel, Len) cursors are attacker-controlled, but the
// safe accessors clamp against the bound parent blobs.
func (l ChainsListView) Bind(nameBlobs, fxIDsBlobs, genesisDataBlobs []byte) BoundChainsList {
	return BoundChainsList{
		view:             l,
		nameBlobs:        nameBlobs,
		fxIDsBlobs:       fxIDsBlobs,
		genesisDataBlobs: genesisDataBlobs,
	}
}

// BoundChainsList is the safe-accessor view over a ChainsListView after
// Bind(). The bound parent blobs are pinned at the type level so
// BoundChainsList.At() returns entries whose Name/FxIDs/GenesisData
// safely slice the blobs.
type BoundChainsList struct {
	view             ChainsListView
	nameBlobs        []byte
	fxIDsBlobs       []byte
	genesisDataBlobs []byte
}

// Len returns the chain count.
func (l BoundChainsList) Len() int { return l.view.Len() }

// IsNull returns true if no list pointer was set.
func (l BoundChainsList) IsNull() bool { return l.view.IsNull() }

// At returns the i'th BoundChainEntry. The bound parent blobs travel
// with the entry so Name/FxIDs/GenesisData safely clamp.
func (l BoundChainsList) At(i int) BoundChainEntry {
	return BoundChainEntry{
		ChainEntry:       l.view.At(i),
		nameBlobs:        l.nameBlobs,
		fxIDsBlobs:       l.fxIDsBlobs,
		genesisDataBlobs: l.genesisDataBlobs,
	}
}

// BoundChainEntry composes ChainEntry with bound parent blobs. Only
// this type exposes safe Name/FxIDs/GenesisData accessors.
type BoundChainEntry struct {
	ChainEntry
	nameBlobs        []byte
	fxIDsBlobs       []byte
	genesisDataBlobs []byte
}

// Name returns the chain's name bytes, clamped against the bound parent
// NameBlobs. Returns empty when the wire (Rel, Len) cursors fall outside
// the parent blob.
func (e BoundChainEntry) Name() []byte {
	rel, length := e.NameRange()
	return safeSlice(e.nameBlobs, rel, length)
}

// FxIDs returns the chain's FxID list as a slice of ids.ID. Each FxID
// is 32 bytes; the entry count = Len / FxIDSize. Returns nil when the
// cursor falls outside or Len is not a multiple of FxIDSize.
func (e BoundChainEntry) FxIDs() []ids.ID {
	rel, length := e.FxIDsRange()
	raw := safeSlice(e.fxIDsBlobs, rel, length)
	if raw == nil || len(raw)%FxIDSize != 0 {
		return nil
	}
	n := len(raw) / FxIDSize
	out := make([]ids.ID, n)
	for i := 0; i < n; i++ {
		copy(out[i][:], raw[i*FxIDSize:(i+1)*FxIDSize])
	}
	return out
}

// GenesisData returns the chain's genesis blob, clamped against the bound
// parent GenesisDataBlobs.
func (e BoundChainEntry) GenesisData() []byte {
	rel, length := e.GenesisDataRange()
	return safeSlice(e.genesisDataBlobs, rel, length)
}

// NewChainsListView reads a ChainsListView from a parent object's field
// offset. Uses the per-stride clamp introduced in zap v0.7.2: a poisoned
// length field that passes the permissive baseline gets rejected here
// (R4V9, LP-023 Red round 4).
//
// CONTRACT: callers MUST call .Bind(nameBlobs, fxIDsBlobs, genesisDataBlobs)
// before using Name/FxIDs/GenesisData accessors. Unbound use returns
// silent-empty slices — a fail-closed default that masks chains.
// The BoundChainsList type enforces this at compile-time.
func NewChainsListView(parent zap.Object, fieldOffset int) ChainsListView {
	return ChainsListView{list: parent.ListStride(fieldOffset, SizeChainEntry)}
}

// MustVerify walks every entry in the list and asserts:
//   - FxIDsLen is an exact multiple of FxIDSize (R6V5).
//   - RESERVED bytes at offsets [56..64) within each entry are all zero
//     (R6-4 / batch 5 v3.5). The writer pads them to zero; a parser that
//     ignores them lets an adversary smuggle non-zero state inside what
//     consensus considers an empty region. Today that's invisible; the
//     day a v4 parser attaches meaning to byte 56 (e.g. a subnet flag),
//     every v3 tx with non-zero reserved bytes silently means two
//     different things on v3 vs v4 nodes — a wire-fork. Reject NOW.
//
// The check is pure cursor inspection — no Bind required, no blob
// slicing. Wired into CreateSovereignL1Tx.Verify (and any future tx
// that embeds a ChainsList).
//
// Returns the first failure wrapped with the entry index. Returns nil
// when the list is empty or every entry passes — empty-list rejection
// is the caller's gate (CreateSovereignL1Tx.Verify enforces non-empty
// via ErrZeroChains before invoking this).
//
// LP-023 Red round 7 R7V8 renamed Verify → MustVerify. The receiver-
// name pattern makes "I forgot to call this" a grep-able regression:
// the audit gate (audit_test.go +
// .github/workflows/zap-audit.yml chainslist-verify-gate) walks every
// file that embeds a ChainsList in a tx type and confirms it calls
// .MustVerify() somewhere inside its Verify() body. The previous
// Verify() name collided with the tx-level Verify() convention and
// invited the reader to assume "the tx Verify already covered this"
// — wrong, because the tx-level Verify is responsible for orchestrating
// the per-field gates, not the list-level walk.
func (l ChainsListView) MustVerify() error {
	n := l.Len()
	for i := 0; i < n; i++ {
		entry := l.At(i)
		_, length := entry.FxIDsRange()
		if length%FxIDSize != 0 {
			return fmt.Errorf("ChainsList[%d].FxIDsLen=%d: %w", i, length, ErrMalformedFxIDsLen)
		}
		// R6-4: reserved bytes [56..64) must all be zero. Read via the
		// raw object surface; bypass IsNull guard because entries we just
		// iterated are in-range by construction (i < l.Len()).
		if entry.IsNull() {
			continue
		}
		for off := OffsetChainEntry_Reserved; off < SizeChainEntry; off++ {
			if entry.obj.Uint8(off) != 0 {
				return fmt.Errorf(
					"ChainsList[%d].Reserved[%d]=0x%02x: %w",
					i, off-OffsetChainEntry_Reserved, entry.obj.Uint8(off), ErrReservedNonZero,
				)
			}
		}
	}
	return nil
}

// ChainsListEntry is the constructor input for a ChainsList. The variable
// fields (Name, FxIDs, GenesisData) are concatenated into the shared blob
// arrays during write; entry header cursors point into them.
type ChainsListEntry struct {
	Name        []byte
	VMID        ids.ID
	FxIDs       []ids.ID
	GenesisData []byte
}

// WriteChainsList writes a chains list and emits its fixed-stride entries
// + the three concatenated sibling blob arrays. Callers MUST emit the
// blob arrays via builder.SetBytes at the parent tx's NameBlobs,
// FxIDsBlobs, and GenesisDataBlobs fields so the Bind() at the reader
// side can reattach them.
//
// Returns:
//   - listOffset: offset of the chain-entry list within the builder.
//   - listCount:  number of chain entries (the wire length field).
//   - nameBlobs / fxIDsBlobs / genesisDataBlobs: the three sibling
//     byte arrays the caller must emit.
func WriteChainsList(b *zap.Builder, entries []ChainsListEntry) (
	listOffset, listCount int,
	nameBlobs []byte,
	fxIDsBlobs []byte,
	genesisDataBlobs []byte,
) {
	if len(entries) == 0 {
		return 0, 0, nil, nil, nil
	}

	// Pre-compute total blob sizes for capacity planning.
	nTotal := 0
	fxTotal := 0
	gTotal := 0
	for _, e := range entries {
		nTotal += len(e.Name)
		fxTotal += len(e.FxIDs) * FxIDSize
		gTotal += len(e.GenesisData)
	}
	nameBlobs = make([]byte, 0, nTotal)
	fxIDsBlobs = make([]byte, 0, fxTotal)
	genesisDataBlobs = make([]byte, 0, gTotal)

	lb := b.StartList(SizeChainEntry)
	nCursor := uint32(0)
	fxCursor := uint32(0)
	gCursor := uint32(0)
	for _, e := range entries {
		var entry [SizeChainEntry]byte

		putU32(entry[OffsetChainEntry_NameRel:], nCursor)
		putU32(entry[OffsetChainEntry_NameLen:], uint32(len(e.Name)))
		nCursor += uint32(len(e.Name))

		for i := 0; i < 32; i++ {
			entry[OffsetChainEntry_VMID+i] = e.VMID[i]
		}

		fxLen := uint32(len(e.FxIDs) * FxIDSize)
		putU32(entry[OffsetChainEntry_FxIDsRel:], fxCursor)
		putU32(entry[OffsetChainEntry_FxIDsLen:], fxLen)
		fxCursor += fxLen

		putU32(entry[OffsetChainEntry_GenesisDataRel:], gCursor)
		putU32(entry[OffsetChainEntry_GenesisDataLen:], uint32(len(e.GenesisData)))
		gCursor += uint32(len(e.GenesisData))

		// Reserved bytes [56..64) remain zero.

		lb.AddBytes(entry[:])

		nameBlobs = append(nameBlobs, e.Name...)
		for _, fx := range e.FxIDs {
			fxIDsBlobs = append(fxIDsBlobs, fx[:]...)
		}
		genesisDataBlobs = append(genesisDataBlobs, e.GenesisData...)
	}
	off, _ := lb.Finish()
	return off, len(entries), nameBlobs, fxIDsBlobs, genesisDataBlobs
}

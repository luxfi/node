// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/zap"
)

// EvidenceEntry is the primitive entry of an EvidenceList. Each entry pins
// the slashable height + evidence type + indices into shared message-blob
// and signature-blob arrays carried as sibling fields of the parent tx.
// Variable-length message/signature bytes live in the shared arrays so the
// list itself stays fixed-stride.
//
// Wire layout per entry (stride 40 bytes; uint64 reads alignment-tolerant):
//
//	Height           uint64 @ 0
//	EvidenceType     uint8  @ 8
//	MessageARel      uint32 @ 12   (index into shared MessageBlobs)
//	MessageALen      uint32 @ 16
//	SignatureARel    uint32 @ 20
//	SignatureALen    uint32 @ 24
//	MessageBRel      uint32 @ 28
//	MessageBLen      uint32 @ 32
//	SignatureBRel    uint32 @ 36   (single 32-bit field; second is in same entry)
//	SignatureBLen    uint32 @ 40
//	Reserved         pad to stride 48
//
// Actually 40-byte width is too cramped; bump stride to 48 for the two
// message + two signature index pairs. The MessageBlobs and SignatureBlobs
// shared arrays are length-prefixed bytes lists; entries index them by
// (start byte, length) per blob.
//
// Final stride: 48 bytes.
const (
	OffsetEvidenceEntry_Height        = 0  // uint64
	OffsetEvidenceEntry_EvidenceType  = 8  // uint8
	OffsetEvidenceEntry_MessageARel   = 12 // uint32 (byte offset within MessageBlobs)
	OffsetEvidenceEntry_MessageALen   = 16 // uint32
	OffsetEvidenceEntry_SignatureARel = 20 // uint32
	OffsetEvidenceEntry_SignatureALen = 24 // uint32
	OffsetEvidenceEntry_MessageBRel   = 28 // uint32
	OffsetEvidenceEntry_MessageBLen   = 32 // uint32
	OffsetEvidenceEntry_SignatureBRel = 36 // uint32
	OffsetEvidenceEntry_SignatureBLen = 40 // uint32
	SizeEvidenceEntry                 = 48
)

// EvidenceEntry is the zero-copy WIRE view over one entry in an
// EvidenceListView. It exposes raw (Rel,Len) cursor accessors only — the
// safe MessageA/B + SignatureA/B accessors live on BoundEvidenceEntry,
// reachable only via EvidenceListView.Bind(...).At(i). Compile-time
// enforcement of NEW-V2 (LP-023 Red round 3).
//
// The raw *Range() accessors below are UNSAFE: they return the wire
// (Rel,Len) verbatim without clamping. Consumers that index parent blobs
// directly with these values can be panicked by an adversary who sets
// Len=0xFFFFFFFF. New code MUST go through BoundEvidenceEntry's safe
// accessors. The Range() helpers remain for legacy internal constructors
// (RED-HIGH-3, LP-023 v3.1 round 2).
type EvidenceEntry struct {
	obj zap.Object
}

// Height returns the height at which the slashable equivocation occurred.
func (e EvidenceEntry) Height() uint64 {
	return e.obj.Uint64(OffsetEvidenceEntry_Height)
}

// EvidenceType returns the equivocation type (e.g. double-vote, double-sign).
// Enum interpretation lives in the executor; the wire layer is opaque.
func (e EvidenceEntry) EvidenceType() uint8 {
	return e.obj.Uint8(OffsetEvidenceEntry_EvidenceType)
}

// MessageARange returns the raw (start, length) wire cursors into the
// parent tx's MessageBlobs array for the first conflicting message.
//
// UNSAFE — call MessageA() instead. The raw (Rel,Len) pair carries no
// clamp; consumers indexing mb[rel:rel+len] panic if an adversary set
// len=0xFFFFFFFF or rel>len(mb). RED-HIGH-3.
func (e EvidenceEntry) MessageARange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_MessageARel),
		e.obj.Uint32(OffsetEvidenceEntry_MessageALen)
}

// SignatureARange returns the raw (start, length) wire cursors for the
// first signature.
//
// UNSAFE — call SignatureA() instead. See MessageARange.
func (e EvidenceEntry) SignatureARange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_SignatureARel),
		e.obj.Uint32(OffsetEvidenceEntry_SignatureALen)
}

// MessageBRange returns the raw (start, length) wire cursors for the
// second conflicting message.
//
// UNSAFE — call MessageB() instead. See MessageARange.
func (e EvidenceEntry) MessageBRange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_MessageBRel),
		e.obj.Uint32(OffsetEvidenceEntry_MessageBLen)
}

// SignatureBRange returns the raw (start, length) wire cursors for the
// second signature.
//
// UNSAFE — call SignatureB() instead. See MessageARange.
func (e EvidenceEntry) SignatureBRange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_SignatureBRel),
		e.obj.Uint32(OffsetEvidenceEntry_SignatureBLen)
}

// safeSlice returns parent[rel:rel+length] clamped against len(parent).
// Any of {rel > len(parent), length > len(parent) - rel, rel+length wraps}
// yields an empty slice. RED-HIGH-3 (LP-023 v3.1 round 2): adversary
// crafts a tx where this slice would panic on the consumer side.
func safeSlice(parent []byte, rel, length uint32) []byte {
	parentLen := uint32(len(parent))
	if rel > parentLen {
		return nil
	}
	if length > parentLen-rel {
		return nil
	}
	return parent[rel : rel+length]
}

// EvidenceListView is the zero-copy WIRE view over a list of EvidenceEntry
// items. The view is UNBOUND — its raw cursor accessors (Range methods on
// EvidenceEntry) are exposed for legacy internal constructors, but the safe
// accessors MessageA/B + SignatureA/B are NOT available on the entries
// returned by EvidenceListView.At().
//
// To get safe accessors, call .Bind(messageBlobs, signatureBlobs) to convert
// to BoundEvidenceList. This is a compile-time enforcement of NEW-V2
// (LP-023 Red round 3 follow-up #1): consumers cannot accidentally read
// raw (Rel,Len) cursors and panic on attacker-set length=0xFFFFFFFF; the
// type system forces them through Bind() first.
type EvidenceListView struct {
	list zap.List
}

// Len returns the entry count.
func (l EvidenceListView) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l EvidenceListView) IsNull() bool { return l.list.IsNull() }

// At returns the i'th EvidenceEntry in raw (unbound) form. The entry's
// Range accessors work; the bound-only MessageA/B + SignatureA/B safe
// accessors are absent. For consumer-side safe access, call .Bind() to
// produce a BoundEvidenceList and iterate via BoundEvidenceList.At().
//
// Returns the zero-value EvidenceEntry when out of range (defensive: a
// clamped ListStride view may have Len()=0 after R4V9 rejected a
// poisoned wire length; constructing an EvidenceEntry from List.Object{}
// on that view would carry a nil msg and panic on downstream field reads).
func (l EvidenceListView) At(i int) EvidenceEntry {
	if i < 0 || i >= l.list.Len() {
		return EvidenceEntry{}
	}
	return EvidenceEntry{obj: l.list.Object(i, SizeEvidenceEntry)}
}

// Bind attaches the parent tx's MessageBlobs and SignatureBlobs and returns
// a BoundEvidenceList whose At() exposes safe MessageA/B + SignatureA/B
// accessors. The raw (Rel,Len) cursors are attacker-controlled, but the
// safe accessors clamp against the bound parent blobs and return empty
// slices on poisoned cursors. RED-HIGH-3 + NEW-V2.
func (l EvidenceListView) Bind(messageBlobs, signatureBlobs []byte) BoundEvidenceList {
	return BoundEvidenceList{
		view:           l,
		messageBlobs:   messageBlobs,
		signatureBlobs: signatureBlobs,
	}
}

// BoundEvidenceList is the safe-accessor view over an EvidenceListView
// after Bind(). The bound parent MessageBlobs + SignatureBlobs are pinned
// at the type level so that BoundEvidenceList.At() returns entries whose
// MessageA/B + SignatureA/B safely slice the blobs (returning empty on
// poisoned cursors). Compile-time enforcement: EvidenceListView lacks the
// safe accessors so consumers cannot bypass Bind() by accident.
type BoundEvidenceList struct {
	view           EvidenceListView
	messageBlobs   []byte
	signatureBlobs []byte
}

// Len returns the entry count.
func (l BoundEvidenceList) Len() int { return l.view.Len() }

// IsNull returns true if no list pointer was set.
func (l BoundEvidenceList) IsNull() bool { return l.view.IsNull() }

// At returns the i'th BoundEvidenceEntry. The bound parent blobs travel
// with the entry so MessageA/B and SignatureA/B safely clamp against the
// blobs returned by the parent tx.
func (l BoundEvidenceList) At(i int) BoundEvidenceEntry {
	return BoundEvidenceEntry{
		EvidenceEntry:  l.view.At(i),
		messageBlobs:   l.messageBlobs,
		signatureBlobs: l.signatureBlobs,
	}
}

// BoundEvidenceEntry composes EvidenceEntry with bound parent blobs. Only
// this type exposes the safe MessageA/B + SignatureA/B accessors —
// EvidenceEntry (raw, unbound) does not. NEW-V2 compile-time enforcement.
type BoundEvidenceEntry struct {
	EvidenceEntry
	messageBlobs   []byte
	signatureBlobs []byte
}

// MessageA returns the first conflicting message bytes, clamped against
// the bound parent MessageBlobs. Returns an empty slice when the wire
// (Rel,Len) cursors fall outside the parent blob.
func (e BoundEvidenceEntry) MessageA() []byte {
	rel, length := e.MessageARange()
	return safeSlice(e.messageBlobs, rel, length)
}

// MessageB returns the second conflicting message bytes, clamped against
// the bound parent MessageBlobs.
func (e BoundEvidenceEntry) MessageB() []byte {
	rel, length := e.MessageBRange()
	return safeSlice(e.messageBlobs, rel, length)
}

// SignatureA returns the first signature bytes, clamped against the bound
// parent SignatureBlobs.
func (e BoundEvidenceEntry) SignatureA() []byte {
	rel, length := e.SignatureARange()
	return safeSlice(e.signatureBlobs, rel, length)
}

// SignatureB returns the second signature bytes, clamped against the
// bound parent SignatureBlobs.
func (e BoundEvidenceEntry) SignatureB() []byte {
	rel, length := e.SignatureBRange()
	return safeSlice(e.signatureBlobs, rel, length)
}

// NewEvidenceListView reads an EvidenceListView from a parent object's
// field offset. Uses the per-stride clamp introduced in zap v0.7.2: a
// poisoned length field that passes the permissive baseline gets rejected
// here (R4V9, LP-023 Red round 4).
//
// CONTRACT: callers MUST call .Bind(messageBlobs, signatureBlobs) before
// using MessageA/B/SignatureA/B accessors. Unbound use returns silent-empty
// slices — a fail-closed default that masks evidence in slashing logic.
// The BoundEvidenceList type enforces this at compile-time: the unbound
// EvidenceListView lacks the safe accessors entirely, forcing consumers
// through Bind() to get a BoundEvidenceList.
func NewEvidenceListView(parent zap.Object, fieldOffset int) EvidenceListView {
	return EvidenceListView{list: parent.ListStride(fieldOffset, SizeEvidenceEntry)}
}

// EvidenceListEntry is the constructor input for an EvidenceList. The four
// byte slices are concatenated into the shared blob arrays during write;
// entry header indices point into them.
type EvidenceListEntry struct {
	Height       uint64
	EvidenceType uint8
	MessageA     []byte
	SignatureA   []byte
	MessageB     []byte
	SignatureB   []byte
}

// WriteEvidenceList writes an evidence list and emits its fixed-stride
// entries. Each entry's MessageA/B + SignatureA/B Rel/Len pairs point into
// the MessageBlobs and SignatureBlobs byte arrays returned by the second
// and third results; callers MUST emit those arrays via
// WriteByteBlobs and store their (offset, length) in the parent tx's
// sibling MessageBlobs and SignatureBlobs fields.
func WriteEvidenceList(b *zap.Builder, entries []EvidenceListEntry) (
	listOffset, listCount int,
	messageBlobs []byte,
	signatureBlobs []byte,
) {
	if len(entries) == 0 {
		return 0, 0, nil, nil
	}

	// Pre-compute total blob sizes for capacity planning.
	mTotal := 0
	sTotal := 0
	for _, e := range entries {
		mTotal += len(e.MessageA) + len(e.MessageB)
		sTotal += len(e.SignatureA) + len(e.SignatureB)
	}
	messageBlobs = make([]byte, 0, mTotal)
	signatureBlobs = make([]byte, 0, sTotal)

	lb := b.StartList(SizeEvidenceEntry)
	mCursor := uint32(0)
	sCursor := uint32(0)
	for _, e := range entries {
		var entry [SizeEvidenceEntry]byte
		putU64(entry[OffsetEvidenceEntry_Height:], e.Height)
		entry[OffsetEvidenceEntry_EvidenceType] = e.EvidenceType

		putU32(entry[OffsetEvidenceEntry_MessageARel:], mCursor)
		putU32(entry[OffsetEvidenceEntry_MessageALen:], uint32(len(e.MessageA)))
		mCursor += uint32(len(e.MessageA))

		putU32(entry[OffsetEvidenceEntry_SignatureARel:], sCursor)
		putU32(entry[OffsetEvidenceEntry_SignatureALen:], uint32(len(e.SignatureA)))
		sCursor += uint32(len(e.SignatureA))

		putU32(entry[OffsetEvidenceEntry_MessageBRel:], mCursor)
		putU32(entry[OffsetEvidenceEntry_MessageBLen:], uint32(len(e.MessageB)))
		mCursor += uint32(len(e.MessageB))

		putU32(entry[OffsetEvidenceEntry_SignatureBRel:], sCursor)
		putU32(entry[OffsetEvidenceEntry_SignatureBLen:], uint32(len(e.SignatureB)))
		sCursor += uint32(len(e.SignatureB))

		lb.AddBytes(entry[:])

		messageBlobs = append(messageBlobs, e.MessageA...)
		messageBlobs = append(messageBlobs, e.MessageB...)
		signatureBlobs = append(signatureBlobs, e.SignatureA...)
		signatureBlobs = append(signatureBlobs, e.SignatureB...)
	}
	off, _ := lb.Finish()
	return off, len(entries), messageBlobs, signatureBlobs
}

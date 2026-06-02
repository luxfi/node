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

// EvidenceEntry is the zero-copy view over one entry in an EvidenceList.
//
// To resolve the (Rel,Len) cursors into actual byte slices the entry must
// be bound to the parent tx's MessageBlobs + SignatureBlobs via
// EvidenceList.Bind(). The bound form exposes safe accessors MessageA(),
// MessageB(), SignatureA(), SignatureB() that clamp against the parent
// blob lengths and return EMPTY slices on poisoned (Rel,Len) pairs.
//
// The raw *Range() accessors below are UNSAFE: they return the wire
// (Rel,Len) verbatim without clamping. Consumers that index parent blobs
// directly with these values can be panicked by an adversary who sets
// Len=0xFFFFFFFF. New code MUST go through the bound MessageA/SignatureA
// safe accessors. The Range() helpers remain for legacy internal
// constructors only (see RED-HIGH-3, LP-023 v3.1 round 2).
type EvidenceEntry struct {
	obj zap.Object
	// Bound parent-blob references. nil for unbound entries (raw range
	// access only); set via EvidenceList.Bind().
	messageBlobs   []byte
	signatureBlobs []byte
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

// MessageA returns the first conflicting message bytes, clamped against
// the bound parent MessageBlobs. Returns an empty slice if the entry is
// unbound or the wire (Rel,Len) cursors fall outside the parent blob.
func (e EvidenceEntry) MessageA() []byte {
	rel, length := e.MessageARange()
	return safeSlice(e.messageBlobs, rel, length)
}

// MessageB returns the second conflicting message bytes, clamped against
// the bound parent MessageBlobs. Returns an empty slice if the entry is
// unbound or the wire (Rel,Len) cursors fall outside the parent blob.
func (e EvidenceEntry) MessageB() []byte {
	rel, length := e.MessageBRange()
	return safeSlice(e.messageBlobs, rel, length)
}

// SignatureA returns the first signature bytes, clamped against the bound
// parent SignatureBlobs. Returns an empty slice if the entry is unbound
// or the wire (Rel,Len) cursors fall outside the parent blob.
func (e EvidenceEntry) SignatureA() []byte {
	rel, length := e.SignatureARange()
	return safeSlice(e.signatureBlobs, rel, length)
}

// SignatureB returns the second signature bytes, clamped against the
// bound parent SignatureBlobs. Returns an empty slice if the entry is
// unbound or the wire (Rel,Len) cursors fall outside the parent blob.
func (e EvidenceEntry) SignatureB() []byte {
	rel, length := e.SignatureBRange()
	return safeSlice(e.signatureBlobs, rel, length)
}

// EvidenceList is the zero-copy view over a list of EvidenceEntry items.
type EvidenceList struct {
	list zap.List
	// Bound parent blobs for safe accessors. nil for raw views; set via
	// Bind().
	messageBlobs   []byte
	signatureBlobs []byte
}

// Len returns the entry count.
func (l EvidenceList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l EvidenceList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th EvidenceEntry, carrying any bound parent blobs.
func (l EvidenceList) At(i int) EvidenceEntry {
	return EvidenceEntry{
		obj:            l.list.Object(i, SizeEvidenceEntry),
		messageBlobs:   l.messageBlobs,
		signatureBlobs: l.signatureBlobs,
	}
}

// Bind attaches the parent tx's MessageBlobs and SignatureBlobs to this
// EvidenceList view so that subsequent At() returns entries that expose
// safe MessageA/B + SignatureA/B accessors. This is the consumer-side
// API for RED-HIGH-3: the raw (Rel,Len) cursors are attacker-controlled,
// but the safe accessors clamp against the bound parent blobs and return
// empty slices on poisoned cursors.
func (l EvidenceList) Bind(messageBlobs, signatureBlobs []byte) EvidenceList {
	l.messageBlobs = messageBlobs
	l.signatureBlobs = signatureBlobs
	return l
}

// EvidenceListView reads an EvidenceList from a parent object's field offset.
// The returned list is UNBOUND — call .Bind(messageBlobs, signatureBlobs)
// to enable safe accessors. Bare At() on an unbound view returns entries
// whose MessageA/B + SignatureA/B return empty slices (clamped against
// nil blobs).
func EvidenceListView(parent zap.Object, fieldOffset int) EvidenceList {
	return EvidenceList{list: parent.List(fieldOffset)}
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

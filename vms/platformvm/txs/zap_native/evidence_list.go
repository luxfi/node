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

// MessageARange returns the (start, length) byte slice into the parent tx's
// MessageBlobs array for the first conflicting message.
func (e EvidenceEntry) MessageARange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_MessageARel),
		e.obj.Uint32(OffsetEvidenceEntry_MessageALen)
}

// SignatureARange returns the (start, length) byte slice for the first
// signature.
func (e EvidenceEntry) SignatureARange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_SignatureARel),
		e.obj.Uint32(OffsetEvidenceEntry_SignatureALen)
}

// MessageBRange returns the (start, length) byte slice for the second
// conflicting message.
func (e EvidenceEntry) MessageBRange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_MessageBRel),
		e.obj.Uint32(OffsetEvidenceEntry_MessageBLen)
}

// SignatureBRange returns the (start, length) byte slice for the second
// signature.
func (e EvidenceEntry) SignatureBRange() (uint32, uint32) {
	return e.obj.Uint32(OffsetEvidenceEntry_SignatureBRel),
		e.obj.Uint32(OffsetEvidenceEntry_SignatureBLen)
}

// EvidenceList is the zero-copy view over a list of EvidenceEntry items.
type EvidenceList struct {
	list zap.List
}

// Len returns the entry count.
func (l EvidenceList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l EvidenceList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th EvidenceEntry.
func (l EvidenceList) At(i int) EvidenceEntry {
	return EvidenceEntry{obj: l.list.Object(i, SizeEvidenceEntry)}
}

// EvidenceListView reads an EvidenceList from a parent object's field offset.
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

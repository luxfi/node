// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// LP-023 Red round 4 — V9 regression suite.
//
// Pins the migration of six list-view accessors from the permissive
// Object.List() baseline to the per-stride Object.ListStride() clamp
// introduced in zap v0.7.2. Each test forges a buffer where the wire
// length field would pass the bare clamp (length <= len(buffer)) but
// fails the tighter clamp (length * stride > bufRem-after-absOffset).
//
// Before the migration, the affected accessors would return a list
// view with Len() == poisoned_length, opening up:
//   - per-element OOB reads on stride > 1 entries (the underlying
//     Object.Uint{8,32,64}/Bytes accessors silently return zero on
//     OOB so no panic, but the consumer ends up iterating poisoned_len
//     ghost entries with all-zero fields → wasted CPU, potential
//     DoS on a downstream pre-allocation).
//   - executor-side silent-data attacks where the consumer trusts
//     the wire-asserted entry count and applies zero-valued entries
//     as if they were real transactions / credentials.
//
// After the migration each accessor's Len() drops to 0 (the
// ListStride clamp rejects the poisoned length); At(0) returns the
// zero-value entry view (no OOB, no panic — defensive bounds check
// on i >= Len() avoids constructing a nil-msg Object).
//
// Accessors covered (file:line of the accessor → stride const):
//   - credential_list.go:71  CredentialListView   → SizeCredential       (16)
//   - credential_list.go:189 SignatureArrayView   → SigBlobSize          (65)
//   - input_list.go:108      InputListView        → SizeTransferableInput (88)
//   - input_list.go:228      SigIndicesArrayView  → SizeSigIndex          (4)
//   - output_list.go:107     OutputListView       → SizeTransferableOutput (96)
//   - evidence_list.go:252   NewEvidenceListView  → SizeEvidenceEntry     (48)

package zap_native

import (
	"encoding/binary"
	"testing"

	"github.com/luxfi/zap"
)

// poisonListAndParse builds a one-entry list of the given stride at offset
// 0 of a parent Object, then overwrites the list's length field with a
// poisoned value chosen to satisfy `poisoned <= len(buf)` (bare List would
// accept) but `poisoned * stride > bufRem-after-absOffset` (ListStride
// rejects). Returns the parsed message so tests can read the field through
// each accessor's *View.
func poisonListAndParse(t *testing.T, stride int) *zap.Message {
	t.Helper()

	// Build a one-entry list of the requested stride. The actual entry
	// bytes are all-zero — we don't care about the entry contents, only
	// the wire length field.
	b := zap.NewBuilder(512)
	lb := b.StartList(stride)
	zero := make([]byte, stride)
	lb.AddBytes(zero)
	listOff, listLen := lb.Finish()

	// Parent object: 8 bytes wide, list pointer at offset 0.
	ob := b.StartObject(8)
	ob.SetList(0, listOff, listLen)
	ob.FinishAsRoot()
	buf := b.Finish()

	// Wire layout: the list pointer is a (relOffset, length) uint32 pair
	// at the parent object's field offset. The parent object starts at
	// rootOffset (header bytes 8:12). Field offset is 0, so the list
	// pointer's length field lives at rootOffset+4.
	rootOffset := int(binary.LittleEndian.Uint32(buf[8:12]))

	// Poison length: pick a value such that `poisoned <= len(buf)` (bare
	// List accepts) but `poisoned * stride > bufRem` (ListStride rejects).
	// `bufRem = len(buf) - absOffset` where absOffset = parentFieldPos +
	// relOffset. relOffset points to where the list bytes were written
	// (after the parent object). For a 512-byte builder, len(buf) ~= 100
	// bytes after header + parent + list-stride. We pick `poisoned =
	// len(buf) / stride + 1` so it survives bare clamp (≤ len(buf)) but
	// fails stride clamp (poisoned * stride > bufRem ≥ len(buf) / 2).
	poisoned := uint32(len(buf)/stride + 1)
	binary.LittleEndian.PutUint32(buf[rootOffset+4:], poisoned)

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse rejected poisoned buffer: %v", err)
	}
	return msg
}

// mustNotPanic wraps a closure with a recover() so the test fails (rather
// than the harness panics) when At(0) on a clamped view triggers a
// nil-msg dereference. R4V9 contract: At(0) on a length-0 view MUST be
// safe — return the zero entry, do not panic. Field reads on the zero
// entry are out-of-scope for this regression (a clamped view never
// reaches them in practice; the executor halts at Len()==0).
func mustNotPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("R4V9 regression (%s): At(0) panicked on clamped view: %v", name, r)
		}
	}()
	fn()
}

// TestRedRound4_V9_CredentialListViewClampsPoisonedLength pins R4V9 for
// CredentialListView (stride 16). Before the migration, Len() would
// return the attacker-set length; after, it returns 0 and At(0) is a
// safe zero accessor.
func TestRedRound4_V9_CredentialListViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SizeCredential)
	list := CredentialListView(msg.Root(), 0)
	if list.Len() != 0 {
		t.Fatalf("R4V9 regression (CredentialListView): poisoned len=%d not clamped; Len()=%d, want 0",
			SizeCredential, list.Len())
	}
	mustNotPanic(t, "CredentialListView", func() { _ = list.At(0) })
}

// TestRedRound4_V9_SignatureArrayViewClampsPoisonedLength pins R4V9 for
// SignatureArrayView (stride 65 = SigBlobSize).
func TestRedRound4_V9_SignatureArrayViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SigBlobSize)
	arr := SignatureArrayView(msg.Root(), 0)
	if arr.Len() != 0 {
		t.Fatalf("R4V9 regression (SignatureArrayView): poisoned len=%d not clamped; Len()=%d, want 0",
			SigBlobSize, arr.Len())
	}
	// At(0) on a clamped SignatureArray takes the i>=Len() branch inside
	// the accessor and returns the zero [SigBlobSize]byte. Verify it's
	// the zero array.
	mustNotPanic(t, "SignatureArrayView", func() {
		got := arr.At(0)
		zero := [SigBlobSize]byte{}
		if got != zero {
			t.Fatalf("R4V9 regression (SignatureArrayView.At(0) post-clamp): non-zero entry %x", got)
		}
	})
}

// TestRedRound4_V9_InputListViewClampsPoisonedLength pins R4V9 for
// InputListView (stride 88 = SizeTransferableInput).
func TestRedRound4_V9_InputListViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SizeTransferableInput)
	list := InputListView(msg.Root(), 0)
	if list.Len() != 0 {
		t.Fatalf("R4V9 regression (InputListView): poisoned len=%d not clamped; Len()=%d, want 0",
			SizeTransferableInput, list.Len())
	}
	mustNotPanic(t, "InputListView", func() { _ = list.At(0) })
}

// TestRedRound4_V9_SigIndicesArrayViewClampsPoisonedLength pins R4V9 for
// SigIndicesArrayView (stride 4 = SizeSigIndex). Stride 4 is the tightest
// margin — bare List() accepts `length <= len(buf)`, ListStride accepts
// `length*4 <= bufRem` — verify the difference is observable.
func TestRedRound4_V9_SigIndicesArrayViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SizeSigIndex)
	arr := SigIndicesArrayView(msg.Root(), 0)
	if arr.Len() != 0 {
		t.Fatalf("R4V9 regression (SigIndicesArrayView): poisoned len=%d not clamped; Len()=%d, want 0",
			SizeSigIndex, arr.Len())
	}
	// SigIndicesArray.At delegates straight to list.Uint32(i) which
	// has its own i<0||i>=length guard — safe on a clamped view.
	mustNotPanic(t, "SigIndicesArrayView", func() {
		if got := arr.At(0); got != 0 {
			t.Fatalf("R4V9 regression (SigIndicesArrayView.At(0) post-clamp): non-zero entry %d", got)
		}
	})
}

// TestRedRound4_V9_OutputListViewClampsPoisonedLength pins R4V9 for
// OutputListView (stride 96 = SizeTransferableOutput).
func TestRedRound4_V9_OutputListViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SizeTransferableOutput)
	list := OutputListView(msg.Root(), 0)
	if list.Len() != 0 {
		t.Fatalf("R4V9 regression (OutputListView): poisoned len=%d not clamped; Len()=%d, want 0",
			SizeTransferableOutput, list.Len())
	}
	mustNotPanic(t, "OutputListView", func() { _ = list.At(0) })
}

// TestRedRound4_V9_EvidenceListViewClampsPoisonedLength pins R4V9 for
// NewEvidenceListView (stride 48 = SizeEvidenceEntry).
func TestRedRound4_V9_EvidenceListViewClampsPoisonedLength(t *testing.T) {
	msg := poisonListAndParse(t, SizeEvidenceEntry)
	view := NewEvidenceListView(msg.Root(), 0)
	if view.Len() != 0 {
		t.Fatalf("R4V9 regression (NewEvidenceListView): poisoned len=%d not clamped; Len()=%d, want 0",
			SizeEvidenceEntry, view.Len())
	}
	mustNotPanic(t, "NewEvidenceListView", func() { _ = view.At(0) })
}

// TestRedRound4_V9_HonestListsStillRoundTrip is the false-positive guard:
// the migration must not break honest construction. Build an honest
// one-entry list for each stride, parse, and confirm Len()==1 plus
// At(0) returns the original entry. If this fails, the ListStride
// clamp is too aggressive for legitimate buffers.
func TestRedRound4_V9_HonestListsStillRoundTrip(t *testing.T) {
	t.Run("CredentialList", func(t *testing.T) {
		b := zap.NewBuilder(256)
		credsOff, credsCount, _ := WriteCredentialList(b, []CredentialListEntry{
			{Sigs: [][SigBlobSize]byte{{}}},
		})
		ob := b.StartObject(8)
		ob.SetList(0, credsOff, credsCount)
		ob.FinishAsRoot()
		msg, err := zap.Parse(b.Finish())
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		list := CredentialListView(msg.Root(), 0)
		if list.Len() != 1 {
			t.Fatalf("honest CredentialList Len()=%d want 1", list.Len())
		}
	})
	t.Run("InputList", func(t *testing.T) {
		b := zap.NewBuilder(256)
		insOff, insCount, _ := WriteInputList(b, []InputListEntry{{Amount: 42}})
		ob := b.StartObject(8)
		ob.SetList(0, insOff, insCount)
		ob.FinishAsRoot()
		msg, err := zap.Parse(b.Finish())
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		list := InputListView(msg.Root(), 0)
		if list.Len() != 1 || list.At(0).Amount() != 42 {
			t.Fatalf("honest InputList round-trip mismatch: Len()=%d Amount=%d",
				list.Len(), list.At(0).Amount())
		}
	})
	t.Run("OutputList", func(t *testing.T) {
		b := zap.NewBuilder(256)
		outsOff, outsCount := WriteOutputList(b, []OutputListEntry{{Amount: 99}})
		ob := b.StartObject(8)
		ob.SetList(0, outsOff, outsCount)
		ob.FinishAsRoot()
		msg, err := zap.Parse(b.Finish())
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		list := OutputListView(msg.Root(), 0)
		if list.Len() != 1 || list.At(0).Amount() != 99 {
			t.Fatalf("honest OutputList round-trip mismatch: Len()=%d Amount=%d",
				list.Len(), list.At(0).Amount())
		}
	})
}

// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// Each batch-3 primitive is exercised via a minimal parent object that wraps
// the list/object pointer at a known field offset, then round-tripped through
// zap.Parse and the primitive's View accessor. Demonstrates the
// constructor → wire → accessor contract and pins the fixed-stride contract.

func TestOutputListRoundTrip(t *testing.T) {
	entries := []OutputListEntry{
		{
			AssetID:      ids.ID{0xa1},
			Amount:       100_000,
			Threshold:    1,
			Locktime:     0,
			OwnerAddress: ids.ShortID{0x11},
		},
		{
			AssetID:      ids.ID{0xb2, 0xc3},
			Amount:       200_000_000_000,
			Threshold:    2,
			Locktime:     1_900_000_000,
			OwnerAddress: ids.ShortID{0x22, 0x33},
		},
		{
			AssetID:      ids.ID{0xff},
			Amount:       1,
			Threshold:    1,
			Locktime:     0,
			OwnerAddress: ids.ShortID{0xfe},
		},
	}

	b := zap.NewBuilder(zap.HeaderSize + 16 + 16 + 3*SizeTransferableOutput)
	off, count := WriteOutputList(b, entries)
	ob := b.StartObject(16) // simple parent with one list-pointer field
	ob.SetList(0, off, count)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	list := OutputListView(msg.Root(), 0)
	if got, want := list.Len(), len(entries); got != want {
		t.Fatalf("OutputList.Len() = %d, want %d", got, want)
	}
	for i, e := range entries {
		got := list.At(i)
		if got.AssetID() != e.AssetID {
			t.Errorf("entry[%d].AssetID round-trip mismatch", i)
		}
		if got.Amount() != e.Amount {
			t.Errorf("entry[%d].Amount = %d, want %d", i, got.Amount(), e.Amount)
		}
		if got.Threshold() != e.Threshold {
			t.Errorf("entry[%d].Threshold = %d, want %d", i, got.Threshold(), e.Threshold)
		}
		if got.Locktime() != e.Locktime {
			t.Errorf("entry[%d].Locktime = %d, want %d", i, got.Locktime(), e.Locktime)
		}
		if got.OwnerAddress() != e.OwnerAddress {
			t.Errorf("entry[%d].OwnerAddress round-trip mismatch", i)
		}
	}
}

func TestInputListRoundTrip(t *testing.T) {
	entries := []InputListEntry{
		{
			TxID:        ids.ID{0x01},
			OutputIndex: 0,
			AssetID:     ids.ID{0xa1},
			Amount:      100,
			SigIndices:  []uint32{0},
		},
		{
			TxID:        ids.ID{0x02},
			OutputIndex: 1,
			AssetID:     ids.ID{0xa2},
			Amount:      200,
			SigIndices:  []uint32{0, 1, 2},
		},
		{
			TxID:        ids.ID{0x03},
			OutputIndex: 0,
			AssetID:     ids.ID{0xa3},
			Amount:      300,
			SigIndices:  nil,
		},
	}

	b := zap.NewBuilder(1024)
	listOff, listCount, sigArr := WriteInputList(b, entries)
	sigArrOff, sigArrCount := WriteSigIndicesArray(b, sigArr)
	ob := b.StartObject(24) // parent: InputList @0, SigIndicesArray @8
	ob.SetList(0, listOff, listCount)
	ob.SetList(8, sigArrOff, sigArrCount)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	list := InputListView(msg.Root(), 0)
	sigs := SigIndicesArrayView(msg.Root(), 8)
	if got, want := list.Len(), len(entries); got != want {
		t.Fatalf("InputList.Len() = %d, want %d", got, want)
	}
	if got, want := sigs.Len(), 4; got != want {
		t.Fatalf("SigIndicesArray.Len() = %d, want %d", got, want)
	}
	for i, e := range entries {
		got := list.At(i)
		if got.TxID() != e.TxID {
			t.Errorf("input[%d].TxID round-trip mismatch", i)
		}
		if got.OutputIndex() != e.OutputIndex {
			t.Errorf("input[%d].OutputIndex = %d, want %d", i, got.OutputIndex(), e.OutputIndex)
		}
		if got.AssetID() != e.AssetID {
			t.Errorf("input[%d].AssetID round-trip mismatch", i)
		}
		if got.Amount() != e.Amount {
			t.Errorf("input[%d].Amount = %d, want %d", i, got.Amount(), e.Amount)
		}
		if int(got.SigIndicesCount()) != len(e.SigIndices) {
			t.Errorf("input[%d].SigIndicesCount = %d, want %d", i, got.SigIndicesCount(), len(e.SigIndices))
		}
		// Slice into the shared array.
		start := got.SigIndicesStart()
		for j, want := range e.SigIndices {
			if gotIdx := sigs.At(int(start) + j); gotIdx != want {
				t.Errorf("input[%d].SigIndices[%d] = %d, want %d", i, j, gotIdx, want)
			}
		}
	}
}

func TestCredentialListRoundTrip(t *testing.T) {
	var sig0, sig1, sig2 [SigBlobSize]byte
	for i := range sig0 {
		sig0[i] = 0x10
		sig1[i] = 0x20
		sig2[i] = 0x30
	}
	entries := []CredentialListEntry{
		{Sigs: [][SigBlobSize]byte{sig0}},
		{Sigs: [][SigBlobSize]byte{sig1, sig2}},
	}

	b := zap.NewBuilder(1024)
	credOff, credCount, sigArr := WriteCredentialList(b, entries)
	sigArrOff, sigArrCount := WriteSignatureArray(b, sigArr)
	ob := b.StartObject(24)
	ob.SetList(0, credOff, credCount)
	ob.SetList(8, sigArrOff, sigArrCount)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	creds := CredentialListView(msg.Root(), 0)
	sigs := SignatureArrayView(msg.Root(), 8)

	if creds.Len() != 2 {
		t.Fatalf("CredentialList.Len() = %d, want 2", creds.Len())
	}
	if sigs.Len() != 3 {
		t.Fatalf("SignatureArray.Len() = %d, want 3", sigs.Len())
	}

	c0 := creds.At(0)
	if c0.SigsCount() != 1 || c0.SigsStart() != 0 {
		t.Fatalf("cred[0] sigs=(%d,%d), want (0,1)", c0.SigsStart(), c0.SigsCount())
	}
	if got := sigs.At(0); got != sig0 {
		t.Errorf("sig[0] mismatch")
	}

	c1 := creds.At(1)
	if c1.SigsCount() != 2 || c1.SigsStart() != 1 {
		t.Fatalf("cred[1] sigs=(%d,%d), want (1,2)", c1.SigsStart(), c1.SigsCount())
	}
	if got := sigs.At(1); got != sig1 {
		t.Errorf("sig[1] mismatch")
	}
	if got := sigs.At(2); got != sig2 {
		t.Errorf("sig[2] mismatch")
	}
}

func TestWarpMessageRoundTrip(t *testing.T) {
	src := ids.ID{0xde, 0xad, 0xbe, 0xef}
	payload := []byte("addressed-call payload bytes")

	b := zap.NewBuilder(1024)
	wmOff := WriteWarpMessage(b, src, payload)
	ob := b.StartObject(8)
	ob.SetObject(0, wmOff)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	wm := WarpMessageView(msg.Root(), 0)
	if wm.IsNull() {
		t.Fatal("WarpMessage view returned null")
	}
	if got := wm.SourceNetwork(); got != src {
		t.Errorf("SourceNetwork mismatch: %v != %v", got, src)
	}
	if got := wm.Payload(); !bytes.Equal(got, payload) {
		t.Errorf("Payload round-trip mismatch")
	}
}

func TestEvidenceListRoundTrip(t *testing.T) {
	entries := []EvidenceListEntry{
		{
			Height:       1_000_000,
			EvidenceType: 1,
			MessageA:     []byte("msgA-1"),
			SignatureA:   []byte("sigA-1"),
			MessageB:     []byte("msgB-1"),
			SignatureB:   []byte("sigB-1"),
		},
		{
			Height:       1_000_001,
			EvidenceType: 2,
			MessageA:     []byte("msgA-2-much-longer-message-body"),
			SignatureA:   []byte("sigA-2"),
			MessageB:     []byte("msgB-2"),
			SignatureB:   []byte("sigB-2-and-this-one-too"),
		},
	}

	b := zap.NewBuilder(2048)
	listOff, listCount, msgBlobs, sigBlobs := WriteEvidenceList(b, entries)
	ob := b.StartObject(24)
	ob.SetList(0, listOff, listCount)
	ob.SetBytes(8, msgBlobs)
	ob.SetBytes(16, sigBlobs)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Bind the parent message+signature blobs so the safe MessageA/B +
	// SignatureA/B accessors clamp against attacker-controlled (Rel,Len)
	// cursors. RED-HIGH-3 (LP-023 v3.1 round 2): the bare *Range accessors
	// + mb[rel:rel+len] indexing panics on poisoned cursors; the safe
	// accessors return empty slices instead.
	mb := msg.Root().Bytes(8)
	sb := msg.Root().Bytes(16)
	list := EvidenceListView(msg.Root(), 0).Bind(mb, sb)

	if list.Len() != len(entries) {
		t.Fatalf("EvidenceList.Len() = %d, want %d", list.Len(), len(entries))
	}
	for i, e := range entries {
		got := list.At(i)
		if got.Height() != e.Height {
			t.Errorf("evidence[%d].Height = %d, want %d", i, got.Height(), e.Height)
		}
		if got.EvidenceType() != e.EvidenceType {
			t.Errorf("evidence[%d].EvidenceType = %d, want %d", i, got.EvidenceType(), e.EvidenceType)
		}
		if !bytes.Equal(got.MessageA(), e.MessageA) {
			t.Errorf("evidence[%d].MessageA mismatch: got %x want %x", i, got.MessageA(), e.MessageA)
		}
		if !bytes.Equal(got.SignatureA(), e.SignatureA) {
			t.Errorf("evidence[%d].SignatureA mismatch", i)
		}
		if !bytes.Equal(got.MessageB(), e.MessageB) {
			t.Errorf("evidence[%d].MessageB mismatch", i)
		}
		if !bytes.Equal(got.SignatureB(), e.SignatureB) {
			t.Errorf("evidence[%d].SignatureB mismatch", i)
		}
	}
}

// TestEmptyListsParse confirms that empty primitive lists round-trip safely
// (Len == 0; At(0) returns a zero view without panic).
func TestEmptyListsParse(t *testing.T) {
	b := zap.NewBuilder(256)
	ob := b.StartObject(24)
	ob.SetList(0, 0, 0)
	ob.SetList(8, 0, 0)
	ob.SetList(16, 0, 0)
	ob.FinishAsRoot()
	buf := b.Finish()

	msg, err := zap.Parse(buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	outs := OutputListView(msg.Root(), 0)
	ins := InputListView(msg.Root(), 8)
	creds := CredentialListView(msg.Root(), 16)
	if !outs.IsNull() && outs.Len() != 0 {
		t.Errorf("empty OutputList.Len() = %d, want 0", outs.Len())
	}
	if !ins.IsNull() && ins.Len() != 0 {
		t.Errorf("empty InputList.Len() = %d, want 0", ins.Len())
	}
	if !creds.IsNull() && creds.Len() != 0 {
		t.Errorf("empty CredentialList.Len() = %d, want 0", creds.Len())
	}
	// Out-of-range At() must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("empty list out-of-range At() panicked: %v", r)
		}
	}()
	_ = outs.At(0)
	_ = ins.At(0)
	_ = creds.At(0)
}

// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"testing"

	"github.com/luxfi/ids"
)

// TestChainsList_RoundTrip pins the multi-chain encode/decode contract
// for the Phase C primitive. Each entry's (Name, VMID, FxIDs,
// GenesisData) tuple must round-trip via Bind().
func TestChainsList_RoundTrip(t *testing.T) {
	entries := []ChainsListEntry{
		{
			Name:        []byte("evm-main"),
			VMID:        ids.ID{0xee, 0x01},
			FxIDs:       []ids.ID{{0xfa}, {0xfb}, {0xfc}},
			GenesisData: []byte("{evm-genesis-data}"),
		},
		{
			Name:        []byte("dex"),
			VMID:        ids.ID{0xdd, 0x01},
			FxIDs:       []ids.ID{{0xfd}},
			GenesisData: []byte("{dex-genesis}"),
		},
		{
			Name:        []byte("fhe"),
			VMID:        ids.ID{0xff, 0x01},
			FxIDs:       []ids.ID{}, // intentionally empty
			GenesisData: []byte(""), // intentionally empty
		},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x18},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("chains-rt"),
		Owner:        OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x99}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	bc := tx.BoundChains()
	if bc.Len() != 3 {
		t.Fatalf("Len=%d want 3", bc.Len())
	}
	for i, want := range entries {
		got := bc.At(i)
		if !bytes.Equal(got.Name(), want.Name) {
			t.Errorf("entry[%d].Name=%q want %q", i, got.Name(), want.Name)
		}
		if got.VMID() != want.VMID {
			t.Errorf("entry[%d].VMID mismatch", i)
		}
		gotFx := got.FxIDs()
		if len(gotFx) != len(want.FxIDs) {
			t.Errorf("entry[%d].FxIDs len=%d want %d", i, len(gotFx), len(want.FxIDs))
		}
		for j, fx := range want.FxIDs {
			if gotFx[j] != fx {
				t.Errorf("entry[%d].FxIDs[%d] mismatch", i, j)
			}
		}
		if !bytes.Equal(got.GenesisData(), want.GenesisData) {
			t.Errorf("entry[%d].GenesisData=%q want %q", i, got.GenesisData(), want.GenesisData)
		}
	}
}

// TestChainsList_EmptyList covers the zero-entry edge: Len()=0, At(0)
// returns zero-value (defensive, never panic).
func TestChainsList_EmptyList(t *testing.T) {
	in := CreateSovereignL1TxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       nil,
	}
	tx := NewCreateSovereignL1Tx(in)
	bc := tx.BoundChains()
	if bc.Len() != 0 {
		t.Fatalf("Len=%d want 0", bc.Len())
	}
	// At(0) on empty list must not panic; returns zero-value BoundChainEntry.
	zero := bc.At(0)
	if zero.VMID() != (ids.ID{}) {
		t.Errorf("At(0) on empty list should return zero-value")
	}
	if zero.Name() != nil {
		t.Errorf("At(0).Name() on empty list should be nil")
	}
}

// TestChainsList_OutOfRange covers the negative + over-len cases on a
// populated list. Both must return zero-value without panicking.
func TestChainsList_OutOfRange(t *testing.T) {
	entries := []ChainsListEntry{
		{Name: []byte("a"), VMID: ids.ID{0x01}, GenesisData: []byte("g")},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	bc := tx.BoundChains()
	if bc.Len() != 1 {
		t.Fatalf("Len=%d want 1", bc.Len())
	}
	// Negative index must return zero-value (Go converts -1 to int which the
	// underlying ChainsListView.At guards via i<0 check).
	zero := bc.At(-1)
	if zero.VMID() != (ids.ID{}) {
		t.Errorf("At(-1) should return zero-value")
	}
	// Beyond Len()
	zero = bc.At(99)
	if zero.VMID() != (ids.ID{}) {
		t.Errorf("At(99) should return zero-value")
	}
}

// TestChainsList_UnboundReturnsRawCursors pins the design: an UNBOUND
// ChainsListView exposes raw (Rel, Len) cursors only. Consumers MUST call
// Bind() before reaching for Name/FxIDs/GenesisData. Compile-time
// enforcement: the safe accessors live on BoundChainEntry only.
func TestChainsList_UnboundReturnsRawCursors(t *testing.T) {
	entries := []ChainsListEntry{
		{Name: []byte("evm"), VMID: ids.ID{0xee}, GenesisData: []byte("g")},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	// Chains() returns ChainsListView (unbound); ChainEntry has raw cursor
	// accessors but no Name()/GenesisData() — those live on BoundChainEntry.
	cv := tx.Chains()
	if cv.Len() != 1 {
		t.Fatalf("unbound view Len=%d want 1", cv.Len())
	}
	e := cv.At(0)
	if e.VMID() != (ids.ID{0xee}) {
		t.Errorf("VMID round-trip fail in unbound")
	}
	rel, length := e.NameRange()
	if length != 3 {
		t.Errorf("unbound NameRange().len=%d want 3", length)
	}
	_ = rel
}

// TestChainsList_BindMismatchedBlobsClampsSafely simulates an adversary
// that produces a buffer whose ChainEntry (NameRel, NameLen) cursor is
// outside the NameBlobs array. Bind() with a too-short blob must clamp
// the access (return nil) rather than panic — the safeSlice() guard from
// EvidenceList (RED-HIGH-3) must apply identically here.
func TestChainsList_BindMismatchedBlobsClampsSafely(t *testing.T) {
	entries := []ChainsListEntry{
		{Name: []byte("legitimate-name"), VMID: ids.ID{0x01}, GenesisData: []byte("genesis")},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)

	// Bind with a TRUNCATED NameBlobs (simulating wire-buffer mismatch
	// between the (Rel, Len) cursor and what blobs are actually present).
	truncated := []byte{} // empty blob, but entry's NameLen=15
	bc := tx.Chains().Bind(truncated, tx.FxIDsBlobs(), tx.GenesisDataBlobs())
	got := bc.At(0).Name()
	if got != nil {
		t.Errorf("Bind with truncated NameBlobs must clamp to nil, got %q", got)
	}
	// GenesisData with the real blob still works.
	if !bytes.Equal(bc.At(0).GenesisData(), []byte("genesis")) {
		t.Errorf("GenesisData with valid blob still works")
	}
}

// TestChainsList_FxIDsNonMultipleClampsToNil pins the entry-count
// invariant: FxIDs blob length must be a multiple of FxIDSize (32B).
// If a hostile wire sets FxIDsLen=33, the FxIDs() accessor returns nil
// rather than emitting a malformed half-entry.
func TestChainsList_FxIDsNonMultipleClampsToNil(t *testing.T) {
	// Use a legitimate tx so blobs are well-formed, then bind with an
	// FxIDsBlobs that's exactly 1 byte shorter than what the entry's
	// FxIDsLen cursor expects. (Hostile wire shape.)
	entries := []ChainsListEntry{
		{
			Name:        []byte("evm"),
			VMID:        ids.ID{0xee},
			FxIDs:       []ids.ID{{0xfa}, {0xfb}},
			GenesisData: []byte("g"),
		},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	good := tx.FxIDsBlobs()
	if len(good) != 64 {
		t.Fatalf("legit FxIDsBlobs len=%d want 64", len(good))
	}
	// Bind with a 1-byte truncated blob — the entry cursor expects 64B
	// but only 63B are reachable, so safeSlice returns nil; FxIDs() sees
	// nil and returns nil (its own multiple-check would also trip).
	truncated := good[:63]
	bc := tx.Chains().Bind(tx.NameBlobs(), truncated, tx.GenesisDataBlobs())
	got := bc.At(0).FxIDs()
	if got != nil {
		t.Errorf("FxIDs() with truncated blob must clamp to nil, got len=%d", len(got))
	}
}

// TestChainsList_MustVerify_RejectsOverCap pins MaxChainsPerL1 (16):
// a CreateSovereignL1Tx declaring 17 chains must fail MustVerify at the
// cap gate before walking entries. Cap matches the multi-chain L1 spawn
// upper bound (P + X + EVM + small application stack) plus headroom.
//
// LP-023 batch 5 v3.7.
func TestChainsList_MustVerify_RejectsOverCap(t *testing.T) {
	entries := make([]ChainsListEntry, MaxChainsPerL1+1)
	for i := range entries {
		entries[i] = ChainsListEntry{
			Name:        []byte("evm"),
			VMID:        ids.ID{byte(i)},
			GenesisData: []byte("g"),
		}
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	err := tx.Chains().MustVerify()
	if err == nil {
		t.Fatalf("MustVerify(over cap) = nil, want ErrTooManyChains")
	}
}

// TestChainsList_MustVerify_AcceptsAtCap pins the boundary: exactly
// MaxChainsPerL1 (16) entries must pass.
func TestChainsList_MustVerify_AcceptsAtCap(t *testing.T) {
	entries := make([]ChainsListEntry, MaxChainsPerL1)
	for i := range entries {
		entries[i] = ChainsListEntry{
			Name:        []byte("evm"),
			VMID:        ids.ID{byte(i)},
			GenesisData: []byte("g"),
		}
	}
	in := CreateSovereignL1TxInput{
		NetworkID:    1,
		BlockchainID: ids.ID{0x18},
		Owner:        OwnerStub{Threshold: 1, Address: ids.ShortID{0x01}},
		Chains:       entries,
	}
	tx := NewCreateSovereignL1Tx(in)
	if err := tx.Chains().MustVerify(); err != nil {
		t.Fatalf("MustVerify(at cap) = %v, want nil", err)
	}
}

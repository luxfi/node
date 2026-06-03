// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"errors"
	"testing"

	"github.com/luxfi/ids"
)

// LP-023 batch 5 v3.5 — three remaining gates from Red round 6 brief.
//
//  - R6-4:  ChainsList RESERVED bytes [56..64) zero-check.
//  - R6-2:  Cross-blob aliasing — documented allowance contract.
//
// The R6-6 AddressList CI gate has its own audit_test.go file (mirrors the
// workflow grep verbatim so a local `go test -race ./...` reproduces CI
// failure).

// TestChainsList_Verify_RejectsNonZeroReserved pins R6-4: a wire buffer
// where any RESERVED byte at offsets [56..64) of any ChainEntry is
// non-zero must be rejected by ChainsListView.Verify. We patch byte 56
// of the first entry — the wire-fork primitive — and prove the gate
// catches it.
func TestChainsList_Verify_RejectsNonZeroReserved(t *testing.T) {
	// Build a legitimate single-chain tx with a unique VMID marker so we
	// can locate the entry in the wire buffer.
	chain := ChainsListEntry{
		Name:        []byte("evm"),
		VMID:        ids.ID{0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44},
		FxIDs:       []ids.ID{{0x01}},
		GenesisData: []byte("g"),
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x42}},
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     []ChainsListEntry{chain},
	})
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}
	buf := tx.Bytes()

	// Locate the chain entry. VMID lives at offset +8 within the entry.
	// Our VMID prefix (8 bytes) is unique enough to anchor the entry
	// start; offset 56 is then 48 bytes later.
	tamper := func(byteIdx int, val byte) []byte {
		out := make([]byte, len(buf))
		copy(out, buf)
		found := false
		for i := 0; i+SizeChainEntry <= len(out); i++ {
			// VMID at +8 within entry; search for unique prefix.
			if out[i+8] != 0xCC || out[i+9] != 0xDD ||
				out[i+10] != 0xEE || out[i+11] != 0xFF ||
				out[i+12] != 0x11 || out[i+13] != 0x22 ||
				out[i+14] != 0x33 || out[i+15] != 0x44 {
				continue
			}
			// Sanity: confirm reserved currently zero.
			if out[i+OffsetChainEntry_Reserved+byteIdx] != 0 {
				continue
			}
			out[i+OffsetChainEntry_Reserved+byteIdx] = val
			found = true
			break
		}
		if !found {
			t.Fatalf("could not locate ChainEntry to tamper byte %d", byteIdx)
		}
		return out
	}

	// Per-byte sweep: flip each of bytes 56..63 individually, prove all
	// eight reject.
	for off := 0; off < (SizeChainEntry - OffsetChainEntry_Reserved); off++ {
		tampered := tamper(off, 0xFF)
		tamperedTx, err := WrapCreateSovereignL1Tx(tampered)
		if err != nil {
			t.Fatalf("offset=%d WrapCreateSovereignL1Tx = %v, want nil (parser permissive)", off, err)
		}
		err = tamperedTx.Verify()
		if !errors.Is(err, ErrReservedNonZero) {
			t.Fatalf("offset=%d Verify = %v, want ErrReservedNonZero", off, err)
		}
	}

	// All-zero reserved (baseline) accepts.
	if err := tx.Verify(); err != nil {
		t.Fatalf("zero-reserved baseline Verify = %v, want nil", err)
	}
}

// TestChainsList_Verify_RejectsNonZeroReserved_TopByte pins the high
// reserved byte specifically — the byte most likely to be ignored by a
// v4 parser that adds a flag at offset 56 only.
func TestChainsList_Verify_RejectsNonZeroReserved_TopByte(t *testing.T) {
	chain := ChainsListEntry{
		Name:        []byte("evm"),
		VMID:        ids.ID{0xAB, 0xCD, 0xEF, 0x01},
		FxIDs:       []ids.ID{{0x01}},
		GenesisData: []byte("g"),
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      OwnerStub{Threshold: 1, Address: ids.ShortID{0x42}},
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     []ChainsListEntry{chain},
	})
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}
	buf := tx.Bytes()
	tampered := make([]byte, len(buf))
	copy(tampered, buf)
	patched := false
	for i := 0; i+SizeChainEntry <= len(tampered); i++ {
		if tampered[i+8] != 0xAB || tampered[i+9] != 0xCD ||
			tampered[i+10] != 0xEF || tampered[i+11] != 0x01 {
			continue
		}
		// Top byte of reserved at +63.
		if tampered[i+OffsetChainEntry_Reserved+7] != 0 {
			continue
		}
		tampered[i+OffsetChainEntry_Reserved+7] = 0x01
		patched = true
		break
	}
	if !patched {
		t.Fatalf("could not locate ChainEntry top-reserved byte")
	}
	tamperedTx, err := WrapCreateSovereignL1Tx(tampered)
	if err != nil {
		t.Fatalf("Wrap = %v", err)
	}
	if err := tamperedTx.Verify(); !errors.Is(err, ErrReservedNonZero) {
		t.Fatalf("Verify(top-reserved=1) = %v, want ErrReservedNonZero", err)
	}
}

// TestChainsList_AllowsOverlappingRanges_DocumentedContract pins R6-2:
// the wire layer ACCEPTS overlapping (rel, len) ranges across distinct
// entries pointing into the shared NameBlobs / FxIDsBlobs /
// GenesisDataBlobs arrays. Two entries with identical cursors return
// byte-equal Name/FxIDs/GenesisData payloads. This is the documented
// allowance: identity is the (VMID, BlockchainID) pair set by the
// executor, NOT the Name bytes.
//
// To exercise the allowance we construct a two-chain wire buffer where
// chain 0 and chain 1 both have `NameRel=0, NameLen=N`. The writer
// (WriteChainsList) concatenates without dedup so the natural path won't
// emit overlapping ranges — we therefore mint a tx with two chains that
// have IDENTICAL Name bytes, then post-encode patch chain 1's NameRel
// to point at chain 0's NameBlob slice (the simplest aliasing case:
// rel=0, len=N). Verify must accept and Name(0) must equal Name(1).
func TestChainsList_AllowsOverlappingRanges_DocumentedContract(t *testing.T) {
	name := []byte("evm-shared-name")
	// Both chains carry identical names — by default the writer writes
	// `name` twice into NameBlobs at offsets 0 and len(name). We'll
	// patch chain 1's NameRel from len(name) to 0 so both point at the
	// same blob slice.
	chain0 := ChainsListEntry{
		Name: name, VMID: ids.ID{0x01}, FxIDs: []ids.ID{{0xfa}}, GenesisData: []byte("g0"),
	}
	chain1 := ChainsListEntry{
		Name: name, VMID: ids.ID{0x02}, FxIDs: []ids.ID{{0xfb}}, GenesisData: []byte("g1"),
	}
	tx := NewCreateSovereignL1Tx(CreateSovereignL1TxInput{
		NetworkID:  1,
		Owner:      OwnerStub{Threshold: 1, Address: ids.ShortID{0x42}},
		Validators: []ValidatorsListEntry{makeValidVerifyingValidator(t)},
		Chains:     []ChainsListEntry{chain0, chain1},
	})
	if err := tx.Verify(); err != nil {
		t.Fatalf("baseline Verify = %v, want nil", err)
	}

	// Patch chain 1's NameRel to 0 (alias with chain 0's slice). Find
	// chain 1 by its unique VMID prefix.
	buf := tx.Bytes()
	tampered := make([]byte, len(buf))
	copy(tampered, buf)
	patched := false
	for i := 0; i+SizeChainEntry <= len(tampered); i++ {
		// Chain 1's VMID byte 0 = 0x02; rest zero. To disambiguate from
		// chain 0, the NameRel at +0 must currently be len(name).
		if tampered[i+8] != 0x02 {
			continue
		}
		zeroTail := true
		for j := 9; j < 8+32; j++ {
			if tampered[i+j] != 0 {
				zeroTail = false
				break
			}
		}
		if !zeroTail {
			continue
		}
		// NameRel (uint32 LE) at +0 of entry should be len(name).
		gotRel := uint32(tampered[i]) | uint32(tampered[i+1])<<8 |
			uint32(tampered[i+2])<<16 | uint32(tampered[i+3])<<24
		if gotRel != uint32(len(name)) {
			continue
		}
		// Patch NameRel to 0.
		tampered[i+0] = 0
		tampered[i+1] = 0
		tampered[i+2] = 0
		tampered[i+3] = 0
		patched = true
		break
	}
	if !patched {
		t.Fatalf("could not locate chain 1 NameRel to alias")
	}
	tamperedTx, err := WrapCreateSovereignL1Tx(tampered)
	if err != nil {
		t.Fatalf("Wrap = %v", err)
	}
	// Verify must ACCEPT — the documented allowance.
	if err := tamperedTx.Verify(); err != nil {
		t.Fatalf("Verify(aliased ranges) = %v, want nil (R6-2 documented allowance)", err)
	}
	// Both entries return byte-equal names — the contract.
	bc := tamperedTx.BoundChains()
	if bc.Len() != 2 {
		t.Fatalf("Len = %d, want 2", bc.Len())
	}
	n0 := bc.At(0).Name()
	n1 := bc.At(1).Name()
	if !bytes.Equal(n0, n1) {
		t.Fatalf("Name(0) = %q, Name(1) = %q — aliased ranges should be byte-equal", n0, n1)
	}
	if !bytes.Equal(n0, name) {
		t.Fatalf("Name(0) = %q, want %q", n0, name)
	}
	// Identity is the VMID, not Name — chain 0 and chain 1 have distinct VMIDs.
	if bc.At(0).VMID() == bc.At(1).VMID() {
		t.Fatalf("VMID(0) == VMID(1) — identity should be distinct")
	}
}

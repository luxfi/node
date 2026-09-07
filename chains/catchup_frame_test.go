// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// catchup_frame_test.go — the CERT-CARRYING catch-up wire format. These prove the
// The property under test: a v2 (block,cert) entry round-trips, an entry with no cert
// routes to the vote path, and a cross-version exchange fails CLEANLY (a legacy
// decoder cannot misparse a v2 frame, and the v2 decoder treats a legacy raw block
// as legacy — never a partial/garbage parse). The cert-accept SEMANTICS are proven
// in the consensus engine tests; here we pin only the framing.
package chains

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCatchupEntry_RoundTrip(t *testing.T) {
	block := []byte("\xf9\x02\x00 a realistic-ish block body")
	cert := []byte("a-marshaled-quorum-cert")

	blk, crt, ok := decodeCatchupEntry(encodeCatchupEntry(block, cert))
	if !ok {
		t.Fatal("a v2 entry must decode as a v2 entry")
	}
	if !bytes.Equal(blk, block) {
		t.Fatalf("block bytes corrupted: got %q want %q", blk, block)
	}
	if !bytes.Equal(crt, cert) {
		t.Fatalf("cert bytes corrupted: got %q want %q", crt, cert)
	}
}

func TestCatchupEntry_EmptyCertRoutesToVotePath(t *testing.T) {
	// A still-pending block served on the live missing-parent path carries no cert.
	// It must decode as a v2 entry with an EMPTY cert — the requester then votes on
	// it (handleContext routes certLen==0 to Put), the legacy behaviour.
	block := []byte("pending-block-no-cert")
	blk, crt, ok := decodeCatchupEntry(encodeCatchupEntry(block, nil))
	if !ok {
		t.Fatal("a v2 entry with empty cert must still decode as v2")
	}
	if !bytes.Equal(blk, block) {
		t.Fatalf("block bytes corrupted: got %q", blk)
	}
	if len(crt) != 0 {
		t.Fatalf("cert must be empty, got %d bytes", len(crt))
	}
}

func TestCatchupEntry_LegacyRawBlockIsNotV2(t *testing.T) {
	// A legacy responder sends the raw block as the container (no magic). The v2
	// decoder must report ok=false so handleContext treats it as a raw block (Put),
	// never as a malformed v2 entry. Cover several real block-prefix shapes.
	for _, raw := range [][]byte{
		{0xf9, 0x02, 0x00, 0x11, 0x22}, // EVM/RLP list header
		{0x00, 0x00, 0x00, 0x2a},       // P/X-chain codec version prefix
		{},                             // empty
		[]byte("LCU"),                  // 3 bytes — too short to even hold the magic
	} {
		if _, _, ok := decodeCatchupEntry(raw); ok {
			t.Fatalf("legacy raw block %x must NOT decode as a v2 entry", raw)
		}
	}
}

func TestCatchupEntry_MagicIsNotAPlausibleLength(t *testing.T) {
	// CROSS-VERSION SAFETY (new responder → legacy requester): a legacy decoder reads
	// the first 4 bytes of a v2 entry as a uint32 length. The magic must be so large
	// that it always exceeds the remaining buffer, so the legacy loop rejects the
	// frame (0 blocks processed) rather than consuming garbage. 0x4C435532 ≈ 1.28 GB.
	asLen := binary.BigEndian.Uint32(catchupEntryMagic[:])
	if asLen < (1 << 30) {
		t.Fatalf("magic read as a length (%d) is too small — a legacy decoder could misparse a v2 frame", asLen)
	}
	// And a full v2 frame's leading length-word (the magic) dwarfs the frame itself,
	// so the legacy `blockLen > remaining` guard always fires.
	frame := encodeCatchupEntry([]byte("blk"), []byte("crt"))
	if uint64(binary.BigEndian.Uint32(frame[:4])) <= uint64(len(frame)) {
		t.Fatal("magic-as-length must exceed the frame length so a legacy decoder self-rejects")
	}
}

func TestCatchupEntry_CorruptV2FramesRejected(t *testing.T) {
	good := encodeCatchupEntry([]byte("block-body"), []byte("cert-body"))

	// Truncations inside the v2 structure must fail to ok=false (never a partial
	// parse): drop the trailing cert byte, drop the certLen word, etc.
	for _, bad := range [][]byte{
		good[:len(good)-1], // last cert byte missing → certLen != remaining
		good[:len(good)-5], // certLen word + cert missing
		good[:8],           // magic + blockLen only, block missing
		good[:6],           // magic + 2 bytes of blockLen
		append(append([]byte(nil), good...), 0x00), // trailing byte → does not consume exactly
	} {
		if _, _, ok := decodeCatchupEntry(bad); ok {
			t.Fatalf("a corrupt v2 frame (len %d) must be rejected, not partial-parsed", len(bad))
		}
	}

	// An overflowing blockLen (claims more block than the buffer holds) is rejected.
	overflow := append([]byte(nil), catchupEntryMagic[:]...)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], 0xFFFFFFFF)
	overflow = append(overflow, u32[:]...)
	overflow = append(overflow, []byte("tiny")...)
	if _, _, ok := decodeCatchupEntry(overflow); ok {
		t.Fatal("a v2 frame with an overflowing blockLen must be rejected")
	}
}

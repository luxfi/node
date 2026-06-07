// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Wire-format regression test. Pins on-chain encoding for every type the
// warp codec is willing to marshal, both as a concrete struct and through
// the Signature interface (which prepends the linearcodec typeID).
//
// The hex strings encoded in `want` are the on-chain bytes. They MUST
// NOT change without a hard fork — any failure here means a code change
// has altered the wire format. The most common ways to break it:
//
//  1. Reordering the RegisterType calls in codec.go (shifts every typeID)
//  2. Adding/removing serializable fields on an existing type
//  3. Changing a serializable field's type
//
// Originally introduced when HybridBLSRTSignature was renamed to
// HybridBLSCoronaSignature; the hex baseline below was captured from the
// pre-rename build and re-verified post-rename. Type and field name
// changes are wire-safe — type IDs and field encodings come from
// registration order and declaration order respectively (see
// reflectcodec/struct_fielder.go and linearcodec/codec.go).

package warp

import (
	"encoding/hex"
	"testing"
)

func TestWireBaselinePreservedForHybridRename(t *testing.T) {
	bls := &BitSetSignature{
		Signers:   []byte{0x01, 0x02, 0x03},
		Signature: [96]byte{0xaa, 0xbb, 0xcc, 0xdd},
	}
	corona := &CoronaSignature{
		Signers:   []byte{0x04, 0x05},
		Signature: []byte{0xee, 0xff, 0x11, 0x22, 0x33},
	}
	enc := &EncryptedWarpPayload{
		EncapsulatedKey: []byte{0x10, 0x11, 0x12},
		Nonce:           []byte{0x20, 0x21},
		Ciphertext:      []byte{0x30, 0x31, 0x32, 0x33},
		RecipientKeyID:  []byte{0x40},
	}
	hybrid := &HybridBLSCoronaSignature{
		Signers:          []byte{0x50, 0x51},
		BLSSignature:     [96]byte{0x60, 0x61, 0x62, 0x63},
		CoronaSignature:  []byte{0x70, 0x71, 0x72},
		CoronaPublicKeys: [][]byte{{0x80, 0x81}, {0x82, 0x83}},
	}

	// Concrete-struct marshal (no typeID prefix, just field-order bytes).
	concrete := func(v interface{}) string {
		b, err := Codec.Marshal(CodecVersion, v)
		if err != nil {
			t.Fatalf("concrete marshal: %v", err)
		}
		return hex.EncodeToString(b)
	}

	// Interface marshal (typeID prefix included, polymorphic).
	asInterface := func(v Signature) string {
		s := v
		b, err := Codec.Marshal(CodecVersion, &s)
		if err != nil {
			t.Fatalf("iface marshal: %v", err)
		}
		return hex.EncodeToString(b)
	}

	// ZAP-native LE baseline (LP-023: codec is LE from genesis). These
	// hex strings ARE the on-chain wire format. Bytes MUST match
	// exactly. zapcodec assigns typeIDs positionally and serializes
	// fields by declaration order — type names and field names are
	// metadata only, never on wire. All multi-byte integers are LE.
	want := map[string]string{
		"CONCRETE BitSetSignature":      "000003000000010203aabbccdd0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"CONCRETE CoronaSignature":      "000002000000040505000000eeff112233",
		"CONCRETE EncryptedWarpPayload": "00000300000010111202000000202104000000303132330100000040",
		"CONCRETE HybridBLSCorona":      "00000200000050516061626300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000300000070717202000000020000008081020000008283",
		"IFACE    BitSetSignature":      "00000000000003000000010203aabbccdd0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"IFACE    CoronaSignature":      "00000100000002000000040505000000eeff112233",
		"IFACE    HybridBLSCorona":      "0000030000000200000050516061626300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000300000070717202000000020000008081020000008283",
	}
	got := map[string]string{
		"CONCRETE BitSetSignature":      concrete(bls),
		"CONCRETE CoronaSignature":      concrete(corona),
		"CONCRETE EncryptedWarpPayload": concrete(enc),
		"CONCRETE HybridBLSCorona":      concrete(hybrid),
		"IFACE    BitSetSignature":      asInterface(bls),
		"IFACE    CoronaSignature":      asInterface(corona),
		"IFACE    HybridBLSCorona":      asInterface(hybrid),
	}
	for name, w := range want {
		g := got[name]
		t.Logf("%-30s = %s", name, g)
		if g != w {
			t.Errorf("WIRE BYTES CHANGED for %s\n  want: %s\n  got:  %s", name, w, g)
		}
	}
}

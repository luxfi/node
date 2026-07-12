// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Wire-format regression test. Pins the native-ZAP struct-is-wire encoding
// for every wkind-dispatched warp type (goldens captured from the encoder at
// the re-genesis cutover that retired the linearcodec wire).
//
// The hex strings in `want` ARE the on-chain bytes. They MUST NOT change
// without a hard fork — any failure here means a code change altered the wire
// format. The most common ways to break it:
//
//  1. Changing a wkind value in wire.go (the discriminator is on-wire DATA)
//  2. Moving a field offset in a marshal/parse pair
//  3. Changing a field's width or encoding
//
// New types append a new wkind; existing bytes never shift.

package warp

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/luxfi/ids"
)

func TestWireBaseline(t *testing.T) {
	bls := &BitSetSignature{Signers: []byte{0x01, 0x02, 0x03}, Signature: [96]byte{0xaa, 0xbb, 0xcc, 0xdd}}
	corona := &CoronaSignature{Signers: []byte{0x04, 0x05}, Signature: []byte{0xee, 0xff, 0x11, 0x22, 0x33}}
	enc := &EncryptedWarpPayload{EncapsulatedKey: []byte{0x10, 0x11, 0x12}, Nonce: []byte{0x20, 0x21}, Ciphertext: []byte{0x30, 0x31, 0x32, 0x33}, RecipientKeyID: []byte{0x40}}
	hybrid := &HybridBLSCoronaSignature{Signers: []byte{0x50, 0x51}, BLSSignature: [96]byte{0x60, 0x61, 0x62, 0x63}, CoronaSignature: []byte{0x70, 0x71, 0x72}, CoronaPublicKeys: [][]byte{{0x80, 0x81}, {0x82, 0x83}}}
	tm := &TeleportMessage{Version: 1, MessageType: TeleportTransfer, SourceChainID: ids.ID{0x01}, DestChainID: ids.ID{0x02}, Nonce: 7, Payload: []byte{0x99, 0x98}, Encrypted: false}
	ttp := &TeleportTransferPayload{AssetID: ids.ID{0x03}, Amount: 1000, Sender: []byte{0x11}, Recipient: []byte{0x22}, Fee: 5, Memo: []byte{0x33}}
	tap := &TeleportAttestPayload{AttestationType: 2, Timestamp: 1234567890, Data: []byte{0x44, 0x55}, AttesterID: ids.NodeID{0x06}}

	mustSig := func(s Signature) []byte {
		b, err := marshalSignature(s)
		if err != nil {
			t.Fatalf("marshalSignature: %v", err)
		}
		return b
	}

	got := map[string][]byte{
		"BitSetSignature":         mustSig(bls),
		"CoronaSignature":         mustSig(corona),
		"HybridBLSCorona":         mustSig(hybrid),
		"EncryptedWarpPayload":    marshalEncryptedWarpPayload(enc),
		"TeleportMessage":         marshalTeleportMessage(tm),
		"TeleportTransferPayload": marshalTeleportTransferPayload(ttp),
		"TeleportAttestPayload":   marshalTeleportAttestPayload(tap),
	}
	want := map[string]string{
		"BitSetSignature":         "5a41500002000000100000007c00000000aabbccdd00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000800000003000000010203",
		"CoronaSignature":         "5a4150000200000010000000280000000110000000020000000a000000050000000405eeff112233",
		"HybridBLSCorona":         "5a4150000200000018000000a200000002000000020000000360616263000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000020000000020000001a0000000300000087ffffff020000000d00000004000000505170717280818283",
		"EncryptedWarpPayload":    "5a41500002000000100000003b0000000220000000030000001b000000020000001500000004000000110000000100000010111220213031323340",
		"TeleportMessage":         "5a4150000200000010000000660000000401000001000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000070000000000000008000000020000009998",
		"TeleportTransferPayload": "5a41500002000000100000005c000000050300000000000000000000000000000000000000000000000000000000000000e8030000000000000500000000000000180000000100000011000000010000000a00000001000000112233",
		"TeleportAttestPayload":   "5a41500002000000100000003800000006020600000000000000000000000000000000000000d20296490000000008000000020000004455",
	}
	wantKind := map[string]wkind{
		"BitSetSignature":         wkindBitSetSignature,
		"CoronaSignature":         wkindCoronaSignature,
		"HybridBLSCorona":         wkindHybridBLSCoronaSignature,
		"EncryptedWarpPayload":    wkindEncryptedWarpPayload,
		"TeleportMessage":         wkindTeleportMessage,
		"TeleportTransferPayload": wkindTeleportTransferPayload,
		"TeleportAttestPayload":   wkindTeleportAttestPayload,
	}

	for name, w := range want {
		g := got[name]
		gHex := hex.EncodeToString(g)
		t.Logf("%-26s = %s", name, gHex)
		if gHex != w {
			t.Errorf("WIRE BYTES CHANGED for %s\n  want: %s\n  got:  %s", name, w, gHex)
		}
		// Structural: the wkind discriminator sits at the root-object offset
		// recorded in the zap header (LE u32 at bytes [8:12]).
		rootOff := binary.LittleEndian.Uint32(g[8:12])
		if k := wkind(g[rootOff]); k != wantKind[name] {
			t.Errorf("wkind for %s = %d, want %d", name, k, wantKind[name])
		}
	}

	// Signature dispatch round-trips: parseSignature must return the same
	// structural value for every signature kind.
	for _, s := range []Signature{bls, corona, hybrid} {
		b := mustSig(s)
		parsed, err := parseSignature(b)
		if err != nil {
			t.Fatalf("parseSignature(%T): %v", s, err)
		}
		reB, err := marshalSignature(parsed)
		if err != nil {
			t.Fatalf("re-marshal %T: %v", s, err)
		}
		if hex.EncodeToString(reB) != hex.EncodeToString(b) {
			t.Errorf("signature %T does not round-trip byte-identically", s)
		}
	}
}

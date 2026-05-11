// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem_test

import (
	"testing"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/node/network/kem"
)

// TestKEMSchemeIDs_AllUseCanonicalNumbering confirms the node's KEM
// package re-exports config.KeyExchangeID byte-for-byte. This anchors
// the Bug 3 fix on the node side: before the fix, this package declared
// ML-KEM-768=0x62 and ML-KEM-1024=0x63, in disagreement with config's
// 0x01 / 0x02. Aliasing brings every wire byte to a single source of
// truth.
func TestKEMSchemeIDs_AllUseCanonicalNumbering(t *testing.T) {
	cases := []struct {
		name    string
		node    kem.KeyExchangeID
		canon   config.KeyExchangeID
		wantHex uint8
	}{
		{"ml-kem-768", kem.KeyExchangeMLKEM768, config.KeyExchangeMLKEM768, 0x01},
		{"ml-kem-1024", kem.KeyExchangeMLKEM1024, config.KeyExchangeMLKEM1024, 0x02},
		{"x25519-unsafe", kem.KeyExchangeX25519Unsafe, config.KeyExchangeX25519Unsafe, 0x90},
		{"none", kem.KeyExchangeNone, config.KeyExchangeInvalid, 0x00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.node != c.canon {
				t.Errorf("node/kem.%s and config.%s diverge: node=0x%02x config=0x%02x",
					c.name, c.name, uint8(c.node), uint8(c.canon))
			}
			if uint8(c.node) != c.wantHex {
				t.Errorf("node/kem.%s wire byte = 0x%02x, want 0x%02x", c.name, uint8(c.node), c.wantHex)
			}
		})
	}
}

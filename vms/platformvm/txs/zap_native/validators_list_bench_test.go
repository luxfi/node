// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

	"github.com/luxfi/ids"
)

// BenchmarkValidatorsList_MustVerify_AllZeroN1024 surfaces the V4
// allocation amplification fix from LP-023 batch 5 v3.8: previously the
// MustVerify floor walk called BLSPubKey()/BLSPoP() per entry, each
// allocating 48B+96B regardless of outcome — at N=1024 with valid BLS
// fields the walk allocates ~144KB before reaching the rejection-trigger
// field. The zero-scan accessors (IsBLSPubKeyZero / IsBLSPoPZero) walk
// the underlying buffer in-place via Object.Uint8 without allocating.
//
// Target: ≤ 3 allocs/op (only the rejection-path fmt.Errorf chain)
// regardless of N (formerly ~2048+ allocs/op at N=1024).
//
// The benchmark constructs 1024 entries with VALID Weight + VALID BLS
// fields + ZERO RegistrationExpiry. This forces MustVerify to walk
// EVERY entry's BLSPubKey + BLSPoP fields before reaching the
// expiry-zero rejection on entry 0 (sequential by index). The legacy
// path allocates 2×144=288B per entry pre-rejection at index 0 alone;
// here the worst case is when the rejection lives at the LAST entry,
// so we wire the expiry-zero on the FINAL entry. That forces the walker
// to read all 1024 BLS fields.
func BenchmarkValidatorsList_MustVerify_AllZeroN1024(b *testing.B) {
	// Non-zero BLS bytes so the BLS-zero check passes; non-zero expiry
	// on every entry except the last so the walker reaches the expiry
	// gate only after walking N-1 valid entries.
	var validPK [BLSPubKeySize]byte
	for i := range validPK {
		validPK[i] = 0xa5
	}
	var validPoP [BLSPoPSize]byte
	for i := range validPoP {
		validPoP[i] = 0x5a
	}
	entries := make([]ValidatorsListEntry, 1024)
	for i := range entries {
		entries[i] = ValidatorsListEntry{
			NodeID:             ids.NodeID{byte(i & 0xff), byte((i >> 8) & 0xff)},
			Weight:             1_000_000,
			BLSPubKey:          validPK,
			BLSPoP:             validPoP,
			RegistrationExpiry: 1_900_000_000,
		}
	}
	// Final entry has zero expiry — triggers rejection only after walking
	// 1024 BLS-zero checks via the zero-scan accessors. Under the legacy
	// path this would allocate 1024×144B for the BLS slice copies.
	entries[len(entries)-1].RegistrationExpiry = 0

	in := ConvertNetworkToL1TxInput{
		NetworkID:      1,
		BlockchainID:   ids.ID{0x18},
		Chain:          ids.ID{0x66},
		ManagerChainID: ids.ID{0x67},
		Address:        []byte{0xab},
		Validators:     entries,
	}
	tx := NewConvertNetworkToL1Tx(in)
	vl := tx.Validators()
	if vl.Len() != 1024 {
		b.Fatalf("setup: Len=%d want 1024", vl.Len())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := vl.MustVerify()
		if err == nil {
			b.Fatal("expected MustVerify rejection on zero expiry of last entry")
		}
	}
}

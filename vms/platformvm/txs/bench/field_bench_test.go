// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"testing"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// BenchmarkFieldAccessLegacy reads the Time field of a parsed legacy
// AdvanceTimeTx. After parse, the field is a direct Go struct deref —
// the lowest theoretical bound a runtime can hit.
func BenchmarkFieldAccessLegacy(b *testing.B) {
	tx := NewAdvanceTimeTxFixture()
	signedBytes := MustMarshalSignedTx(tx)
	parsed, err := txs.Parse(txs.Codec, signedBytes)
	if err != nil {
		b.Fatal(err)
	}
	unsigned := parsed.Unsigned.(*txs.AdvanceTimeTx)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = unsigned.Time
	}
}

// BenchmarkFieldAccessNative reads the Time field of a wrapped
// native-ZAP AdvanceTimeTx via offset arithmetic. Should be
// comparable to a struct deref — a single 8-byte little-endian load.
func BenchmarkFieldAccessNative(b *testing.B) {
	tx := zap_native.NewAdvanceTimeTx(1782604800)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = tx.Time()
	}
}

// BenchmarkFieldAccess1MBatch reads the Time field 1M times for each
// codec to amortize benchmark frame overhead. The per-read cost in
// the per-op bench is below 2 ns and at that scale the testing
// framework's per-iteration overhead matters; batching escapes it.
func BenchmarkFieldAccess1MBatch(b *testing.B) {
	legacyTx := NewAdvanceTimeTxFixture()
	legacyBytes := MustMarshalSignedTx(legacyTx)
	legacyParsed, err := txs.Parse(txs.Codec, legacyBytes)
	if err != nil {
		b.Fatal(err)
	}
	legacyUnsigned := legacyParsed.Unsigned.(*txs.AdvanceTimeTx)
	zapTx := zap_native.NewAdvanceTimeTx(1782604800)

	b.Run("legacy_1M_reads", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var acc uint64
			for j := 0; j < 1_000_000; j++ {
				acc ^= legacyUnsigned.Time
			}
			if acc == 0xdeadbeefdeadbeef {
				b.Fatal("impossible")
			}
		}
	})
	b.Run("zap_1M_reads", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var acc uint64
			for j := 0; j < 1_000_000; j++ {
				acc ^= zapTx.Time()
			}
			if acc == 0xdeadbeefdeadbeef {
				b.Fatal("impossible")
			}
		}
	})
}

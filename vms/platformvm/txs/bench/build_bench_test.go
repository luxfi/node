// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"testing"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// BenchmarkBuildLegacy measures fields→bytes via the legacy
// reflection codec Marshal path for each fixture. Building includes the
// reflection-cache lookup, packer alloc, and per-field walk.
func BenchmarkBuildLegacy(b *testing.B) {
	fixtures := FixtureMap()
	names := sortedFixtureNames(fixtures)
	for _, name := range names {
		unsigned := fixtures[name]
		signedTx := &txs.Tx{Unsigned: unsigned}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := txs.Codec.Marshal(txs.CodecVersion, signedTx)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) == 0 {
					b.Fatal("empty marshal")
				}
				b.SetBytes(int64(len(out)))
			}
		})
	}
}

// BenchmarkBuildNativeAdvanceTime measures the AdvanceTimeTx build
// path through the native-ZAP builder Blue landed. Directly comparable
// to BenchmarkBuildLegacy/AdvanceTimeTx — same single uint64 field.
//
// Native: zap_native.NewAdvanceTimeTx writes the time at a fixed
// offset, no reflection, no codec dispatch.
func BenchmarkBuildNativeAdvanceTime(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := zap_native.NewAdvanceTimeTx(uint64(i))
		out := tx.Bytes()
		if len(out) == 0 {
			b.Fatal("empty build")
		}
	}
}

// sortedFixtureNames returns the fixture names in deterministic order.
func sortedFixtureNames(m map[string]txs.UnsignedTx) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	return names
}

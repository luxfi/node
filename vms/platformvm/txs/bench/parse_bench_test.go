// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"sort"
	"testing"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// BenchmarkParseLegacy walks the fixture map and benchmarks Parse for
// each type via the legacy linearcodec path (txs.Parse). The output
// names the tx type so the per-type table can be assembled from the
// raw `go test -bench` output.
func BenchmarkParseLegacy(b *testing.B) {
	preencoded := preencodeFixtures()
	for _, name := range sortedNames(preencoded) {
		name := name
		signedBytes := preencoded[name]
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(signedBytes)))
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := txs.Parse(txs.Codec, signedBytes)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParseNativeAdvanceTime measures the AdvanceTimeTx path
// through the native-ZAP implementation that Blue already landed.
// Numbers are directly comparable to BenchmarkParseLegacy/AdvanceTimeTx
// because both fixtures encode the same single uint64 field.
//
// Native-ZAP fixture: a fresh zap_native.NewAdvanceTimeTx builds the
// canonical 32-byte buffer; WrapAdvanceTimeTx parses it (zero-copy).
//
// This is the ONLY tx type that has a native-ZAP path as of 2026-06-02.
// The rest of the parse table is legacy-only; future native paths land
// alongside corresponding benches in this file.
func BenchmarkParseNativeAdvanceTime(b *testing.B) {
	tx := zap_native.NewAdvanceTimeTx(1782604800)
	zapBytes := tx.Bytes()
	b.SetBytes(int64(len(zapBytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrapped, err := zap_native.WrapAdvanceTimeTx(zapBytes)
		if err != nil {
			b.Fatal(err)
		}
		_ = wrapped.Time()
	}
}

// preencodeFixtures encodes every fixture once and returns the map of
// name → signed bytes the parse benches walk over. Encoding happens
// outside the timed loop so we measure parse cost only.
func preencodeFixtures() map[string][]byte {
	m := FixtureMap()
	out := make(map[string][]byte, len(m))
	for name, unsigned := range m {
		out[name] = MustMarshalSignedTx(unsigned)
	}
	return out
}

// sortedNames returns the keys of m in deterministic order so the
// per-type bench output is stable across runs.
func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
)

// Legacy AdvanceTimeTx layout for benchmark comparison.
//
// This mirrors the actual platformvm/txs.AdvanceTimeTx shape stripped to its
// single Time field. We could import the real type, but doing so introduces a
// cyclic-dependency through the txs package's init(); the field shape is the
// only thing the bench cares about so a local mirror keeps the benchmark
// hermetic.
type legacyAdvanceTimeTx struct {
	Time uint64 `serialize:"true"`
}

// newLegacyManager builds a codec.Manager with our local stub type registered
// so the benchmark exercises the real linearcodec reflection path.
func newLegacyManager(b interface{ Fatal(...any) }) codec.Manager {
	c := linearcodec.NewDefault()
	if err := c.RegisterType(&legacyAdvanceTimeTx{}); err != nil {
		b.Fatal(err)
	}
	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(0, c); err != nil {
		b.Fatal(err)
	}
	return m
}

// BenchmarkParse_Legacy measures the cost of unmarshaling an AdvanceTimeTx
// via the linearcodec reflection-driven path.
func BenchmarkParse_Legacy(b *testing.B) {
	m := newLegacyManager(b)
	src := &legacyAdvanceTimeTx{Time: 1782604800}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyAdvanceTimeTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
		_ = dst.Time
	}
}

// BenchmarkParse_ZAP measures the cost of wrapping a ZAP-encoded buffer
// in a typed accessor (zero-copy).
func BenchmarkParse_ZAP(b *testing.B) {
	tx := NewAdvanceTimeTx(1782604800)
	buf := tx.Bytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t2, err := WrapAdvanceTimeTx(buf)
		if err != nil {
			b.Fatal(err)
		}
		_ = t2.Time()
	}
}

// BenchmarkBuild_Legacy measures the cost of marshaling an AdvanceTimeTx
// via the linearcodec reflection-driven path.
func BenchmarkBuild_Legacy(b *testing.B) {
	m := newLegacyManager(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyAdvanceTimeTx{Time: uint64(i)}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuild_ZAP measures the cost of constructing an AdvanceTimeTx
// via direct ZAP offset writes (no reflection, no codec lookup).
func BenchmarkBuild_ZAP(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewAdvanceTimeTx(uint64(i))
		_ = tx.Bytes()
	}
}

// BenchmarkFieldAccess_Legacy measures field read on a pre-parsed legacy
// struct. This is the lower bound — pure pointer deref, no codec involvement.
// It's the baseline for comparing the ZAP zero-copy accessor read against.
func BenchmarkFieldAccess_Legacy(b *testing.B) {
	tx := &legacyAdvanceTimeTx{Time: 1782604800}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tx.Time
	}
}

// BenchmarkFieldAccess_ZAP measures field read on a ZAP-wrapped accessor.
// Should be in the same order of magnitude as the legacy struct deref —
// the ZAP read is a binary.LittleEndian.Uint64 at a known offset, vs the
// legacy is a struct-field load. Both compile to a few instructions.
func BenchmarkFieldAccess_ZAP(b *testing.B) {
	tx := NewAdvanceTimeTx(1782604800)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tx.Time()
	}
}

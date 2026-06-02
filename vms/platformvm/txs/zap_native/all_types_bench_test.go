// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/ids"
)

// Stub structs mirroring the field shape of each platformvm tx type.
// Used for the legacy linearcodec parse/build benchmarks so we measure the
// reflection-walk cost against an equivalent field set.

type legacyRewardValidatorTx struct {
	TxID ids.ID `serialize:"true"`
}

type legacySetL1ValidatorWeightTx struct {
	ValidationID ids.ID `serialize:"true"`
	Nonce        uint64 `serialize:"true"`
	Weight       uint64 `serialize:"true"`
}

type legacyIncreaseL1ValidatorBalanceTx struct {
	ValidationID ids.ID `serialize:"true"`
	Balance      uint64 `serialize:"true"`
}

type legacyDisableL1ValidatorTx struct {
	ValidationID ids.ID `serialize:"true"`
}

// newLegacyManagerFor builds a codec.Manager with the given types registered.
func newLegacyManagerFor(b *testing.B, types ...interface{}) codec.Manager {
	c := linearcodec.NewDefault()
	for _, t := range types {
		if err := c.RegisterType(t); err != nil {
			b.Fatal(err)
		}
	}
	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(0, c); err != nil {
		b.Fatal(err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────
// RewardValidatorTx
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_RewardValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRewardValidatorTx{})
	src := &legacyRewardValidatorTx{TxID: ids.ID{1, 2, 3, 4, 5}}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyRewardValidatorTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_RewardValidatorTx(b *testing.B) {
	tx := NewRewardValidatorTx(ids.ID{1, 2, 3, 4, 5})
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapRewardValidatorTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_RewardValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRewardValidatorTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyRewardValidatorTx{TxID: ids.ID{byte(i)}}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_RewardValidatorTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewRewardValidatorTx(ids.ID{byte(i)})
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// SetL1ValidatorWeightTx
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_SetL1ValidatorWeightTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacySetL1ValidatorWeightTx{})
	src := &legacySetL1ValidatorWeightTx{ValidationID: ids.ID{0xaa}, Nonce: 42, Weight: 1000}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacySetL1ValidatorWeightTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_SetL1ValidatorWeightTx(b *testing.B) {
	tx := NewSetL1ValidatorWeightTx(ids.ID{0xaa}, 42, 1000)
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapSetL1ValidatorWeightTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_SetL1ValidatorWeightTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacySetL1ValidatorWeightTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacySetL1ValidatorWeightTx{ValidationID: ids.ID{byte(i)}, Nonce: uint64(i), Weight: uint64(i)}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_SetL1ValidatorWeightTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewSetL1ValidatorWeightTx(ids.ID{byte(i)}, uint64(i), uint64(i))
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// IncreaseL1ValidatorBalanceTx
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_IncreaseL1ValidatorBalanceTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyIncreaseL1ValidatorBalanceTx{})
	src := &legacyIncreaseL1ValidatorBalanceTx{ValidationID: ids.ID{0xbb}, Balance: 5000}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyIncreaseL1ValidatorBalanceTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_IncreaseL1ValidatorBalanceTx(b *testing.B) {
	tx := NewIncreaseL1ValidatorBalanceTx(ids.ID{0xbb}, 5000)
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapIncreaseL1ValidatorBalanceTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_IncreaseL1ValidatorBalanceTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyIncreaseL1ValidatorBalanceTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyIncreaseL1ValidatorBalanceTx{ValidationID: ids.ID{byte(i)}, Balance: uint64(i)}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_IncreaseL1ValidatorBalanceTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewIncreaseL1ValidatorBalanceTx(ids.ID{byte(i)}, uint64(i))
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// DisableL1ValidatorTx
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_DisableL1ValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyDisableL1ValidatorTx{})
	src := &legacyDisableL1ValidatorTx{ValidationID: ids.ID{0xcc}}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyDisableL1ValidatorTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_DisableL1ValidatorTx(b *testing.B) {
	tx := NewDisableL1ValidatorTx(ids.ID{0xcc})
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapDisableL1ValidatorTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_DisableL1ValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyDisableL1ValidatorTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyDisableL1ValidatorTx{ValidationID: ids.ID{byte(i)}}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_DisableL1ValidatorTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewDisableL1ValidatorTx(ids.ID{byte(i)})
		_ = tx.Bytes()
	}
}

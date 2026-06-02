// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/crypto/bls"
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

// Batch 2 legacy stubs — same field shape as the v1 native schemas above.
type legacyBaseTx struct {
	NetworkID    uint32 `serialize:"true"`
	BlockchainID ids.ID `serialize:"true"`
	Memo         []byte `serialize:"true"`
}

type legacyRegisterL1ValidatorTx struct {
	ValidationID            ids.ID                 `serialize:"true"`
	BLSPublicKey            [bls.PublicKeyLen]byte `serialize:"true"`
	ProofOfPossession       [bls.SignatureLen]byte `serialize:"true"`
	Expiry                  uint64                 `serialize:"true"`
	RemainingBalanceOwnerID ids.ID                 `serialize:"true"`
}

type legacySlashValidatorTx struct {
	NodeID          ids.NodeID `serialize:"true"`
	Chain           ids.ID     `serialize:"true"`
	SlashPercentage uint32     `serialize:"true"`
}

type legacyTransferChainOwnershipTx struct {
	Chain          ids.ID      `serialize:"true"`
	OwnerThreshold uint32      `serialize:"true"`
	OwnerLocktime  uint64      `serialize:"true"`
	OwnerAddress   ids.ShortID `serialize:"true"`
}

type legacyRemoveChainValidatorTx struct {
	NodeID ids.NodeID `serialize:"true"`
	Chain  ids.ID     `serialize:"true"`
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

// ─────────────────────────────────────────────────────────────────────────
// BaseTx (Batch 2)
// ─────────────────────────────────────────────────────────────────────────

var benchBaseTxMemo = []byte("LP-023 batch 2 realistic memo payload for parse/build cost measurement")

func BenchmarkParse_Legacy_BaseTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyBaseTx{})
	src := &legacyBaseTx{NetworkID: 1337, BlockchainID: ids.ID{0x11}, Memo: benchBaseTxMemo}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyBaseTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_BaseTx(b *testing.B) {
	tx := NewBaseTx(1337, ids.ID{0x11}, benchBaseTxMemo)
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapBaseTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_BaseTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyBaseTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyBaseTx{NetworkID: uint32(i), BlockchainID: ids.ID{byte(i)}, Memo: benchBaseTxMemo}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_BaseTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewBaseTx(uint32(i), ids.ID{byte(i)}, benchBaseTxMemo)
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// RegisterL1ValidatorTx (Batch 2)
// ─────────────────────────────────────────────────────────────────────────

var (
	benchRegBLS [bls.PublicKeyLen]byte
	benchRegPoP [bls.SignatureLen]byte
)

func init() {
	for i := range benchRegBLS {
		benchRegBLS[i] = byte(i + 1)
	}
	for i := range benchRegPoP {
		benchRegPoP[i] = byte(0xff - i)
	}
}

func BenchmarkParse_Legacy_RegisterL1ValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRegisterL1ValidatorTx{})
	src := &legacyRegisterL1ValidatorTx{
		ValidationID:            ids.ID{0xaa},
		BLSPublicKey:            benchRegBLS,
		ProofOfPossession:       benchRegPoP,
		Expiry:                  1_900_000_000,
		RemainingBalanceOwnerID: ids.ID{0xbb},
	}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyRegisterL1ValidatorTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_RegisterL1ValidatorTx(b *testing.B) {
	tx := NewRegisterL1ValidatorTx(ids.ID{0xaa}, benchRegBLS, benchRegPoP, 1_900_000_000, ids.ID{0xbb})
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapRegisterL1ValidatorTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_RegisterL1ValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRegisterL1ValidatorTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyRegisterL1ValidatorTx{
			ValidationID:            ids.ID{byte(i)},
			BLSPublicKey:            benchRegBLS,
			ProofOfPossession:       benchRegPoP,
			Expiry:                  uint64(i),
			RemainingBalanceOwnerID: ids.ID{byte(i)},
		}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_RegisterL1ValidatorTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewRegisterL1ValidatorTx(ids.ID{byte(i)}, benchRegBLS, benchRegPoP, uint64(i), ids.ID{byte(i)})
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// SlashValidatorTx (Batch 2)
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_SlashValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacySlashValidatorTx{})
	src := &legacySlashValidatorTx{NodeID: ids.NodeID{0xa1}, Chain: ids.ID{0xa2}, SlashPercentage: 100_000}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacySlashValidatorTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_SlashValidatorTx(b *testing.B) {
	tx := NewSlashValidatorTx(ids.NodeID{0xa1}, ids.ID{0xa2}, 100_000)
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapSlashValidatorTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_SlashValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacySlashValidatorTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacySlashValidatorTx{NodeID: ids.NodeID{byte(i)}, Chain: ids.ID{byte(i)}, SlashPercentage: uint32(i)}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_SlashValidatorTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewSlashValidatorTx(ids.NodeID{byte(i)}, ids.ID{byte(i)}, uint32(i))
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// TransferChainOwnershipTx (Batch 2)
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_TransferChainOwnershipTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyTransferChainOwnershipTx{})
	src := &legacyTransferChainOwnershipTx{Chain: ids.ID{0xc0}, OwnerThreshold: 1, OwnerLocktime: 0, OwnerAddress: ids.ShortID{0xbe, 0xef}}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyTransferChainOwnershipTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_TransferChainOwnershipTx(b *testing.B) {
	tx := NewTransferChainOwnershipTx(ids.ID{0xc0}, 1, 0, ids.ShortID{0xbe, 0xef})
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapTransferChainOwnershipTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_TransferChainOwnershipTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyTransferChainOwnershipTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyTransferChainOwnershipTx{Chain: ids.ID{byte(i)}, OwnerThreshold: uint32(i), OwnerLocktime: uint64(i), OwnerAddress: ids.ShortID{byte(i)}}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_TransferChainOwnershipTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewTransferChainOwnershipTx(ids.ID{byte(i)}, uint32(i), uint64(i), ids.ShortID{byte(i)})
		_ = tx.Bytes()
	}
}

// ─────────────────────────────────────────────────────────────────────────
// RemoveChainValidatorTx (Batch 2)
// ─────────────────────────────────────────────────────────────────────────

func BenchmarkParse_Legacy_RemoveChainValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRemoveChainValidatorTx{})
	src := &legacyRemoveChainValidatorTx{NodeID: ids.NodeID{0x10}, Chain: ids.ID{0xfa}}
	encoded, err := m.Marshal(0, src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dst legacyRemoveChainValidatorTx
		if _, err := m.Unmarshal(encoded, &dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse_ZAP_RemoveChainValidatorTx(b *testing.B) {
	tx := NewRemoveChainValidatorTx(ids.NodeID{0x10}, ids.ID{0xfa})
	buf := tx.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := WrapRemoveChainValidatorTx(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_Legacy_RemoveChainValidatorTx(b *testing.B) {
	m := newLegacyManagerFor(b, &legacyRemoveChainValidatorTx{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := &legacyRemoveChainValidatorTx{NodeID: ids.NodeID{byte(i)}, Chain: ids.ID{byte(i)}}
		if _, err := m.Marshal(0, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuild_ZAP_RemoveChainValidatorTx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx := NewRemoveChainValidatorTx(ids.NodeID{byte(i)}, ids.ID{byte(i)})
		_ = tx.Bytes()
	}
}

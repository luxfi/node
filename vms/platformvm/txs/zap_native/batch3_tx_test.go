// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// Helper to build a non-trivial sample outs/ins/creds set used by multiple
// round-trip tests.
func sampleOuts() []OutputListEntry {
	return []OutputListEntry{
		{AssetID: ids.ID{0xa1}, Amount: 100, Threshold: 1, Locktime: 0, OwnerAddress: ids.ShortID{0x11}},
		{AssetID: ids.ID{0xa2}, Amount: 200, Threshold: 1, Locktime: 1_900_000_000, OwnerAddress: ids.ShortID{0x22}},
	}
}

func sampleIns() []InputListEntry {
	return []InputListEntry{
		{TxID: ids.ID{0x01}, OutputIndex: 0, AssetID: ids.ID{0xa1}, Amount: 100, SigIndices: []uint32{0}},
		{TxID: ids.ID{0x02}, OutputIndex: 1, AssetID: ids.ID{0xa2}, Amount: 200, SigIndices: []uint32{0, 1}},
	}
}

func sampleCreds() []CredentialListEntry {
	var s0, s1, s2 [SigBlobSize]byte
	for i := range s0 {
		s0[i] = 0xA0
		s1[i] = 0xB0
		s2[i] = 0xC0
	}
	return []CredentialListEntry{
		{Sigs: [][SigBlobSize]byte{s0}},
		{Sigs: [][SigBlobSize]byte{s1, s2}},
	}
}

func TestBaseTxFullRoundTrip(t *testing.T) {
	in := BaseTxFullInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0xc1},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("phase C canary"),
	}

	tx := NewBaseTxFull(in)
	if !IsZAPBytes(tx.Bytes()) {
		t.Fatal("BaseTxFull bytes not ZAP-formatted")
	}
	if tx.NetworkID() != in.NetworkID {
		t.Errorf("NetworkID round-trip")
	}
	if tx.BlockchainID() != in.BlockchainID {
		t.Errorf("BlockchainID round-trip")
	}
	if !bytes.Equal(tx.Memo(), in.Memo) {
		t.Errorf("Memo round-trip")
	}
	if tx.Outs().Len() != len(in.Outs) {
		t.Fatalf("Outs.Len() = %d, want %d", tx.Outs().Len(), len(in.Outs))
	}
	if tx.Ins().Len() != len(in.Ins) {
		t.Fatalf("Ins.Len() = %d, want %d", tx.Ins().Len(), len(in.Ins))
	}
	if tx.Credentials().Len() != len(in.Credentials) {
		t.Fatalf("Credentials.Len() = %d, want %d", tx.Credentials().Len(), len(in.Credentials))
	}
	if tx.SigIndicesArray().Len() != 3 {
		t.Fatalf("SigIndicesArray.Len() = %d, want 3", tx.SigIndicesArray().Len())
	}
	if tx.SignatureArray().Len() != 3 {
		t.Fatalf("SignatureArray.Len() = %d, want 3", tx.SignatureArray().Len())
	}

	tx2, err := WrapBaseTxFull(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	if tx2.NetworkID() != in.NetworkID || tx2.BlockchainID() != in.BlockchainID {
		t.Fatal("wrap-round-trip mismatch")
	}
	// Cross-confusion: WrapBaseTx (the metadata-only stub) must reject.
	if _, err := WrapBaseTx(tx.Bytes()); err != ErrWrongTxKind {
		t.Fatalf("WrapBaseTx on BaseTxFull bytes: want ErrWrongTxKind, got %v", err)
	}
}

func TestImportTxRoundTrip(t *testing.T) {
	in := ImportTxInput{
		NetworkID:     1337,
		BlockchainID:  ids.ID{0xc1},
		SourceNetwork: ids.ID{0xd1, 0xd2},
		Outs:          sampleOuts(),
		LocalIns:      sampleIns(),
		ImportedIns: []InputListEntry{
			{TxID: ids.ID{0x03}, OutputIndex: 0, AssetID: ids.ID{0xa3}, Amount: 999, SigIndices: []uint32{0}},
		},
		Credentials: sampleCreds(),
		Memo:        []byte("import"),
	}

	tx := NewImportTx(in)
	if tx.SourceNetwork() != in.SourceNetwork {
		t.Errorf("SourceNetwork round-trip")
	}
	wantStart, wantCount := uint32(len(in.LocalIns)), uint32(len(in.ImportedIns))
	gotStart, gotCount := tx.ImportedInsRange()
	if gotStart != wantStart || gotCount != wantCount {
		t.Fatalf("ImportedInsRange = (%d,%d), want (%d,%d)", gotStart, gotCount, wantStart, wantCount)
	}
	// Combined Ins length includes both local and imported.
	if tx.Ins().Len() != len(in.LocalIns)+len(in.ImportedIns) {
		t.Fatalf("Ins.Len() = %d, want %d", tx.Ins().Len(), len(in.LocalIns)+len(in.ImportedIns))
	}

	tx2, err := WrapImportTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	if tx2.SourceNetwork() != in.SourceNetwork {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestExportTxRoundTrip(t *testing.T) {
	in := ExportTxInput{
		NetworkID:          1337,
		BlockchainID:       ids.ID{0xc1},
		DestinationNetwork: ids.ID{0xe1, 0xe2},
		LocalOuts:          sampleOuts(),
		ExportedOuts: []OutputListEntry{
			{AssetID: ids.ID{0xa9}, Amount: 500, Threshold: 1, Locktime: 0, OwnerAddress: ids.ShortID{0x99}},
		},
		Ins:         sampleIns(),
		Credentials: sampleCreds(),
		Memo:        []byte("export"),
	}

	tx := NewExportTx(in)
	if tx.DestinationNetwork() != in.DestinationNetwork {
		t.Errorf("DestinationNetwork round-trip")
	}
	wantStart, wantCount := uint32(len(in.LocalOuts)), uint32(len(in.ExportedOuts))
	gotStart, gotCount := tx.ExportedOutsRange()
	if gotStart != wantStart || gotCount != wantCount {
		t.Fatalf("ExportedOutsRange = (%d,%d), want (%d,%d)", gotStart, gotCount, wantStart, wantCount)
	}

	tx2, err := WrapExportTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	if tx2.DestinationNetwork() != in.DestinationNetwork {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestAddPermissionlessValidatorTxRoundTrip(t *testing.T) {
	var blsPub [bls.PublicKeyLen]byte
	for i := range blsPub {
		blsPub[i] = byte(i + 1)
	}
	var pop [bls.SignatureLen]byte
	for i := range pop {
		pop[i] = byte(0xFF - i)
	}

	in := AddPermissionlessValidatorTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0xc1},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("apv"),

		NodeID:               ids.NodeID{0xaa, 0xbb, 0xcc},
		StakeStart:           1_700_000_000,
		StakeEnd:             1_800_000_000,
		StakeWeight:          1_000_000,
		StakeAssetID:         ids.ID{0xaf},
		StakeAmount:          2_000_000_000,
		BLSPublicKey:         blsPub,
		BLSProofOfPossession: pop,

		ValidationRewardsOwner: OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x10}},
		DelegationRewardsOwner: OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x20}},
		DelegationShares:       100_000,
	}

	tx := NewAddPermissionlessValidatorTx(in)
	if tx.NodeID() != in.NodeID {
		t.Errorf("NodeID round-trip")
	}
	if tx.StakeStart() != in.StakeStart {
		t.Errorf("StakeStart round-trip")
	}
	if tx.StakeEnd() != in.StakeEnd {
		t.Errorf("StakeEnd round-trip")
	}
	if tx.StakeWeight() != in.StakeWeight {
		t.Errorf("StakeWeight round-trip")
	}
	if tx.StakeAssetID() != in.StakeAssetID {
		t.Errorf("StakeAssetID round-trip")
	}
	if tx.StakeAmount() != in.StakeAmount {
		t.Errorf("StakeAmount round-trip")
	}
	if tx.BLSPublicKey() != blsPub {
		t.Errorf("BLSPublicKey round-trip")
	}
	if tx.BLSProofOfPossession() != pop {
		t.Errorf("BLSProofOfPossession round-trip")
	}
	vrThresh, vrLT, vrAddr := tx.ValidationRewardsOwner()
	if vrThresh != in.ValidationRewardsOwner.Threshold ||
		vrLT != in.ValidationRewardsOwner.Locktime ||
		vrAddr != in.ValidationRewardsOwner.Address {
		t.Errorf("ValidationRewardsOwner round-trip")
	}
	drThresh, drLT, drAddr := tx.DelegationRewardsOwner()
	if drThresh != in.DelegationRewardsOwner.Threshold ||
		drLT != in.DelegationRewardsOwner.Locktime ||
		drAddr != in.DelegationRewardsOwner.Address {
		t.Errorf("DelegationRewardsOwner round-trip")
	}
	if tx.DelegationShares() != in.DelegationShares {
		t.Errorf("DelegationShares round-trip")
	}

	tx2, err := WrapAddPermissionlessValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	if tx2.NodeID() != in.NodeID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestCreateChainTxRoundTrip(t *testing.T) {
	in := CreateChainTxInput{
		NetworkID:       1337,
		BlockchainID:    ids.ID{0xc1},
		Outs:            sampleOuts(),
		Ins:             sampleIns(),
		Credentials:     sampleCreds(),
		Memo:            []byte("cc"),
		ParentNetwork:   ids.ID{0xf1},
		VMID:            ids.ID{0xf2},
		Owner:           OwnerStub{Threshold: 1, Locktime: 1_900_000_000, Address: ids.ShortID{0x77}},
		WarpMessageHash: ids.ID{0xa1, 0xa2, 0xa3, 0xa4},
		GenesisData:     []byte("genesis bytes for the new chain"),
	}

	tx := NewCreateChainTx(in)
	if tx.NetworkID() != in.NetworkID {
		t.Errorf("NetworkID round-trip")
	}
	if tx.ParentNetwork() != in.ParentNetwork {
		t.Errorf("ParentNetwork round-trip")
	}
	if tx.VMID() != in.VMID {
		t.Errorf("VMID round-trip")
	}
	if tx.WarpMessageHash() != in.WarpMessageHash {
		t.Errorf("WarpMessageHash round-trip")
	}
	if !bytes.Equal(tx.GenesisData(), in.GenesisData) {
		t.Errorf("GenesisData round-trip")
	}
	ownerT, ownerLT, ownerAddr := tx.Owner()
	if ownerT != in.Owner.Threshold || ownerLT != in.Owner.Locktime || ownerAddr != in.Owner.Address {
		t.Errorf("Owner round-trip")
	}

	tx2, err := WrapCreateChainTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	if tx2.VMID() != in.VMID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

// TestBatch3CrossConfusionStaysClosed verifies all 5 new tx types reject
// every other type's bytes with ErrWrongTxKind, preserving the v3 TxKind
// invariant across the expanded set.
func TestBatch3CrossConfusionStaysClosed(t *testing.T) {
	baseFull := NewBaseTxFull(BaseTxFullInput{NetworkID: 1, BlockchainID: ids.ID{1}}).Bytes()
	importTx := NewImportTx(ImportTxInput{NetworkID: 1, BlockchainID: ids.ID{1}}).Bytes()
	exportTx := NewExportTx(ExportTxInput{NetworkID: 1, BlockchainID: ids.ID{1}}).Bytes()
	apv := NewAddPermissionlessValidatorTx(AddPermissionlessValidatorTxInput{
		NetworkID: 1, BlockchainID: ids.ID{1},
	}).Bytes()
	cc := NewCreateChainTx(CreateChainTxInput{NetworkID: 1, BlockchainID: ids.ID{1}}).Bytes()

	// Pairwise: every WrapX(bufY) where Y != X must return ErrWrongTxKind.
	tests := []struct {
		name string
		fn   func() error
	}{
		{"WrapBaseTxFull(importTx)", func() error { _, err := WrapBaseTxFull(importTx); return err }},
		{"WrapImportTx(baseFull)", func() error { _, err := WrapImportTx(baseFull); return err }},
		{"WrapExportTx(importTx)", func() error { _, err := WrapExportTx(importTx); return err }},
		{"WrapAddPermissionlessValidatorTx(cc)", func() error {
			_, err := WrapAddPermissionlessValidatorTx(cc)
			return err
		}},
		{"WrapCreateChainTx(apv)", func() error { _, err := WrapCreateChainTx(apv); return err }},
		{"WrapBaseTx(baseFull)", func() error { _, err := WrapBaseTx(baseFull); return err }},
		{"WrapAdvanceTimeTx(exportTx)", func() error { _, err := WrapAdvanceTimeTx(exportTx); return err }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err != ErrWrongTxKind {
				t.Fatalf("%s: got %v, want ErrWrongTxKind", tc.name, err)
			}
		})
	}
}

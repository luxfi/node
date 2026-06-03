// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"testing"

	"github.com/luxfi/ids"
)

// TestAddValidatorTxRoundTrip pins AddValidatorTx round-trip (pre-Etna
// primary network validator add).
func TestAddValidatorTxRoundTrip(t *testing.T) {
	in := AddValidatorTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x11},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("add-v"),
		NodeID:       ids.NodeID{0xa1, 0xa2, 0xa3},
		StakeStart:   1_700_000_000,
		StakeEnd:     1_800_000_000,
		StakeWeight:  500_000,
		StakeOuts:    sampleOuts(),
		RewardsOwner: OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x55}},
		DelegationShares: 200_000,
	}

	tx := NewAddValidatorTx(in)
	if tx.NetworkID() != in.NetworkID {
		t.Errorf("NetworkID round-trip")
	}
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
	rT, rLT, rAddr := tx.RewardsOwner()
	if rT != in.RewardsOwner.Threshold || rLT != in.RewardsOwner.Locktime || rAddr != in.RewardsOwner.Address {
		t.Errorf("RewardsOwner round-trip")
	}
	if tx.DelegationShares() != in.DelegationShares {
		t.Errorf("DelegationShares round-trip")
	}
	if !bytes.Equal(tx.Memo(), in.Memo) {
		t.Errorf("Memo round-trip")
	}

	tx2, err := WrapAddValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.NodeID() != in.NodeID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestAddDelegatorTxRoundTrip(t *testing.T) {
	in := AddDelegatorTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x12},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("add-d"),
		NodeID:       ids.NodeID{0xb1, 0xb2, 0xb3},
		StakeStart:   1_700_000_000,
		StakeEnd:     1_800_000_000,
		StakeWeight:  300_000,
		StakeOuts:    sampleOuts(),
		DelegationRewardsOwner: OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x66}},
	}

	tx := NewAddDelegatorTx(in)
	if tx.NodeID() != in.NodeID {
		t.Errorf("NodeID round-trip")
	}
	dT, dLT, dAddr := tx.DelegationRewardsOwner()
	if dT != in.DelegationRewardsOwner.Threshold || dLT != in.DelegationRewardsOwner.Locktime || dAddr != in.DelegationRewardsOwner.Address {
		t.Errorf("DelegationRewardsOwner round-trip")
	}
	tx2, err := WrapAddDelegatorTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.NodeID() != in.NodeID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestAddPermissionlessDelegatorTxRoundTrip(t *testing.T) {
	in := AddPermissionlessDelegatorTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x13},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("add-pd"),
		NodeID:       ids.NodeID{0xc1, 0xc2, 0xc3},
		StakeStart:   1_700_000_000,
		StakeEnd:     1_800_000_000,
		StakeWeight:  400_000,
		Chain:        ids.ID{0xe1, 0xe2, 0xe3},
		StakeAssetID: ids.ID{0xaf},
		StakeOuts:    sampleOuts(),
		DelegationRewardsOwner: OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x77}},
	}
	tx := NewAddPermissionlessDelegatorTx(in)
	if tx.Chain() != in.Chain {
		t.Errorf("Chain round-trip")
	}
	if tx.StakeAssetID() != in.StakeAssetID {
		t.Errorf("StakeAssetID round-trip")
	}
	tx2, err := WrapAddPermissionlessDelegatorTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.Chain() != in.Chain {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestAddChainValidatorTxRoundTrip(t *testing.T) {
	in := AddChainValidatorTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x14},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("add-cv"),
		NodeID:       ids.NodeID{0xd1, 0xd2, 0xd3},
		StakeStart:   1_700_000_000,
		StakeEnd:     1_800_000_000,
		StakeWeight:  600_000,
		Chain:        ids.ID{0xf1},
	}
	tx := NewAddChainValidatorTx(in)
	if tx.Chain() != in.Chain {
		t.Errorf("Chain round-trip")
	}
	if tx.NodeID() != in.NodeID {
		t.Errorf("NodeID round-trip")
	}
	tx2, err := WrapAddChainValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.Chain() != in.Chain {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestCreateNetworkTxRoundTrip(t *testing.T) {
	in := CreateNetworkTxInput{
		NetworkID:    1337,
		BlockchainID: ids.ID{0x15},
		Outs:         sampleOuts(),
		Ins:          sampleIns(),
		Credentials:  sampleCreds(),
		Memo:         []byte("cnet"),
		Owner:        OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x88}},
	}
	tx := NewCreateNetworkTx(in)
	if tx.NetworkID() != in.NetworkID {
		t.Errorf("NetworkID round-trip")
	}
	oT, oLT, oA := tx.Owner()
	if oT != in.Owner.Threshold || oLT != in.Owner.Locktime || oA != in.Owner.Address {
		t.Errorf("Owner round-trip")
	}
	tx2, err := WrapCreateNetworkTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.NetworkID() != in.NetworkID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestTransformChainTxRoundTrip(t *testing.T) {
	in := TransformChainTxInput{
		NetworkID:                1337,
		BlockchainID:             ids.ID{0x16},
		Outs:                     sampleOuts(),
		Ins:                      sampleIns(),
		Credentials:              sampleCreds(),
		Memo:                     []byte("tc"),
		Chain:                    ids.ID{0xc1},
		AssetID:                  ids.ID{0xa1, 0xa2},
		InitialSupply:            1_000_000_000,
		MaximumSupply:            10_000_000_000,
		MinConsumptionRate:       100_000,
		MaxConsumptionRate:       500_000,
		MinValidatorStake:        2_000_000_000,
		MaxValidatorStake:        3_000_000_000,
		MinStakeDuration:         86400,
		MaxStakeDuration:         86400 * 365,
		MinDelegationFee:         20_000,
		MinDelegatorStake:        25_000_000,
		MaxValidatorWeightFactor: 5,
		UptimeRequirement:        600_000,
	}
	tx := NewTransformChainTx(in)
	if tx.InitialSupply() != in.InitialSupply {
		t.Errorf("InitialSupply round-trip")
	}
	if tx.MaximumSupply() != in.MaximumSupply {
		t.Errorf("MaximumSupply round-trip")
	}
	if tx.MinValidatorStake() != in.MinValidatorStake {
		t.Errorf("MinValidatorStake round-trip")
	}
	if tx.MaxValidatorStake() != in.MaxValidatorStake {
		t.Errorf("MaxValidatorStake round-trip")
	}
	if tx.MinStakeDuration() != in.MinStakeDuration {
		t.Errorf("MinStakeDuration round-trip")
	}
	if tx.MaxStakeDuration() != in.MaxStakeDuration {
		t.Errorf("MaxStakeDuration round-trip")
	}
	if tx.MinDelegationFee() != in.MinDelegationFee {
		t.Errorf("MinDelegationFee round-trip")
	}
	if tx.MaxValidatorWeightFactor() != in.MaxValidatorWeightFactor {
		t.Errorf("MaxValidatorWeightFactor round-trip")
	}
	if tx.UptimeRequirement() != in.UptimeRequirement {
		t.Errorf("UptimeRequirement round-trip")
	}
	tx2, err := WrapTransformChainTx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.Chain() != in.Chain {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestConvertNetworkToL1TxRoundTrip(t *testing.T) {
	addr := []byte{0xde, 0xad, 0xbe, 0xef, 0x12, 0x34}
	in := ConvertNetworkToL1TxInput{
		NetworkID:      1337,
		BlockchainID:   ids.ID{0x17},
		Outs:           sampleOuts(),
		Ins:            sampleIns(),
		Credentials:    sampleCreds(),
		Memo:           []byte("conv"),
		Chain:          ids.ID{0xc1, 0xc2},
		ManagerChainID: ids.ID{0xb1, 0xb2},
		Address:        addr,
	}
	tx := NewConvertNetworkToL1Tx(in)
	if tx.Chain() != in.Chain {
		t.Errorf("Chain round-trip")
	}
	if tx.ManagerChainID() != in.ManagerChainID {
		t.Errorf("ManagerChainID round-trip")
	}
	if !bytes.Equal(tx.Address(), addr) {
		t.Errorf("Address round-trip")
	}
	tx2, err := WrapConvertNetworkToL1Tx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.Chain() != in.Chain {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestCreateSovereignL1TxRoundTrip(t *testing.T) {
	mgrAddr := []byte{0xab, 0xcd, 0xef, 0x01}
	// Multi-chain L1 (Phase C): EVM + DEX
	chains := []ChainsListEntry{
		{
			Name:        []byte("evm"),
			VMID:        ids.ID{0xee, 0x01},
			FxIDs:       []ids.ID{{0xfa}, {0xfb}},
			GenesisData: []byte("{evm-genesis}"),
		},
		{
			Name:        []byte("dex"),
			VMID:        ids.ID{0xee, 0x02},
			FxIDs:       []ids.ID{{0xfc}},
			GenesisData: []byte("{dex-genesis}"),
		},
	}
	// Initial validators (Phase D): 2 validators
	vals := []ValidatorsListEntry{
		{
			NodeID: ids.NodeID{0xa1, 0xa2, 0xa3},
			Weight: 1_000_000,
			BLSPubKey: [BLSPubKeySize]byte{0xb1, 0xb2},
			BLSPoP: [BLSPoPSize]byte{0xc1, 0xc2},
			RegistrationExpiry: 1_900_000_000,
		},
		{
			NodeID: ids.NodeID{0xa4, 0xa5},
			Weight: 2_000_000,
			BLSPubKey: [BLSPubKeySize]byte{0xb3, 0xb4},
			BLSPoP: [BLSPoPSize]byte{0xc3, 0xc4},
			RegistrationExpiry: 1_950_000_000,
		},
	}
	in := CreateSovereignL1TxInput{
		NetworkID:       1337,
		BlockchainID:    ids.ID{0x18},
		Outs:            sampleOuts(),
		Ins:             sampleIns(),
		Credentials:     sampleCreds(),
		Memo:            []byte("sov"),
		Owner:           OwnerStub{Threshold: 1, Locktime: 0, Address: ids.ShortID{0x99}},
		ManagerChainIdx: 0,
		ManagerAddress:  mgrAddr,
		Validators:      vals,
		Chains:          chains,
	}
	tx := NewCreateSovereignL1Tx(in)
	oT, oLT, oA := tx.Owner()
	if oT != in.Owner.Threshold || oLT != in.Owner.Locktime || oA != in.Owner.Address {
		t.Errorf("Owner round-trip")
	}
	if tx.ManagerChainIdx() != in.ManagerChainIdx {
		t.Errorf("ManagerChainIdx round-trip")
	}
	if !bytes.Equal(tx.ManagerAddress(), mgrAddr) {
		t.Errorf("ManagerAddress round-trip")
	}

	// Phase C: chains round-trip via Bind()
	bc := tx.BoundChains()
	if bc.Len() != 2 {
		t.Fatalf("Chains.Len=%d want 2", bc.Len())
	}
	for i, want := range chains {
		got := bc.At(i)
		if !bytes.Equal(got.Name(), want.Name) {
			t.Errorf("Chain[%d].Name=%q want %q", i, got.Name(), want.Name)
		}
		if got.VMID() != want.VMID {
			t.Errorf("Chain[%d].VMID round-trip", i)
		}
		gotFx := got.FxIDs()
		if len(gotFx) != len(want.FxIDs) {
			t.Errorf("Chain[%d].FxIDs len=%d want %d", i, len(gotFx), len(want.FxIDs))
		}
		for j, fx := range want.FxIDs {
			if gotFx[j] != fx {
				t.Errorf("Chain[%d].FxIDs[%d] round-trip", i, j)
			}
		}
		if !bytes.Equal(got.GenesisData(), want.GenesisData) {
			t.Errorf("Chain[%d].GenesisData=%q want %q", i, got.GenesisData(), want.GenesisData)
		}
	}

	// Phase D: validators round-trip
	vl := tx.Validators()
	if vl.Len() != 2 {
		t.Fatalf("Validators.Len=%d want 2", vl.Len())
	}
	for i, want := range vals {
		got := vl.At(i)
		if got.NodeID() != want.NodeID {
			t.Errorf("Val[%d].NodeID round-trip", i)
		}
		if got.Weight() != want.Weight {
			t.Errorf("Val[%d].Weight=%d want %d", i, got.Weight(), want.Weight)
		}
		if !bytes.Equal(got.BLSPubKey(), want.BLSPubKey[:]) {
			t.Errorf("Val[%d].BLSPubKey round-trip", i)
		}
		if !bytes.Equal(got.BLSPoP(), want.BLSPoP[:]) {
			t.Errorf("Val[%d].BLSPoP round-trip", i)
		}
		if got.RegistrationExpiry() != want.RegistrationExpiry {
			t.Errorf("Val[%d].RegistrationExpiry round-trip", i)
		}
	}

	tx2, err := WrapCreateSovereignL1Tx(tx.Bytes())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if tx2.BoundChains().Len() != 2 {
		t.Fatal("wrap-round-trip chains mismatch")
	}
	if tx2.Validators().Len() != 2 {
		t.Fatal("wrap-round-trip validators mismatch")
	}
}

// TestBatch4_TxKindCrossTypeRejection pins the cross-type confusion
// defense for all batch 4 tx types: each Wrap*Tx must reject buffers
// whose TxKind discriminator does not match the expected kind.
func TestBatch4_TxKindCrossTypeRejection(t *testing.T) {
	// Build an AddValidatorTx and try to wrap it as every other batch 4
	// kind — all must return ErrWrongTxKind.
	av := NewAddValidatorTx(AddValidatorTxInput{
		NetworkID: 1, NodeID: ids.NodeID{0x1},
		RewardsOwner: OwnerStub{Threshold: 1},
	})
	buf := av.Bytes()
	if _, err := WrapAddDelegatorTx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapAddDelegatorTx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapAddPermissionlessDelegatorTx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapAddPermissionlessDelegatorTx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapAddChainValidatorTx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapAddChainValidatorTx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapCreateNetworkTx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapCreateNetworkTx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapTransformChainTx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapTransformChainTx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapConvertNetworkToL1Tx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapConvertNetworkToL1Tx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
	if _, err := WrapCreateSovereignL1Tx(buf); err != ErrWrongTxKind {
		t.Errorf("WrapCreateSovereignL1Tx(AV bytes) = %v, want ErrWrongTxKind", err)
	}
}

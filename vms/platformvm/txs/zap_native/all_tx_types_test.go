// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"bytes"
	"testing"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// Round-trip parity tests for the L1-management tx types.
//
// Each test builds via the typed builder, asserts non-zero buffer + ZAP
// magic, re-wraps the buffer in a fresh accessor, and checks field equality.

func TestRewardValidatorTxRoundTrip(t *testing.T) {
	want := ids.ID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

	tx := NewRewardValidatorTx(want)
	if !IsZAPBytes(tx.Bytes()) {
		t.Fatal("Bytes() not ZAP-formatted")
	}
	if got := tx.TxID(); got != want {
		t.Fatalf("TxID() = %v, want %v", got, want)
	}

	tx2, err := WrapRewardValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := tx2.TxID(); got != want {
		t.Fatalf("round-trip TxID() = %v, want %v", got, want)
	}
}

func TestSetL1ValidatorWeightTxRoundTrip(t *testing.T) {
	id := ids.ID{0xab, 0xcd, 0xef}
	const wantNonce uint64 = 42
	const wantWeight uint64 = 1_000_000_000

	tx := NewSetL1ValidatorWeightTx(id, wantNonce, wantWeight)
	if tx.ValidationID() != id {
		t.Fatalf("ValidationID round-trip mismatch")
	}
	if tx.Nonce() != wantNonce {
		t.Fatalf("Nonce = %d, want %d", tx.Nonce(), wantNonce)
	}
	if tx.Weight() != wantWeight {
		t.Fatalf("Weight = %d, want %d", tx.Weight(), wantWeight)
	}

	tx2, err := WrapSetL1ValidatorWeightTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.ValidationID() != id || tx2.Nonce() != wantNonce || tx2.Weight() != wantWeight {
		t.Fatalf("wrap-round-trip mismatch")
	}
}

func TestIncreaseL1ValidatorBalanceTxRoundTrip(t *testing.T) {
	id := ids.ID{0x77, 0x88, 0x99}
	const want uint64 = 5_000_000

	tx := NewIncreaseL1ValidatorBalanceTx(id, want)
	if tx.ValidationID() != id {
		t.Fatal("ValidationID mismatch")
	}
	if tx.Balance() != want {
		t.Fatalf("Balance = %d, want %d", tx.Balance(), want)
	}
}

func TestDisableL1ValidatorTxRoundTrip(t *testing.T) {
	id := ids.ID{0xde, 0xad, 0xbe, 0xef}

	tx := NewDisableL1ValidatorTx(id)
	if tx.ValidationID() != id {
		t.Fatal("ValidationID mismatch")
	}

	tx2, err := WrapDisableL1ValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.ValidationID() != id {
		t.Fatal("round-trip mismatch")
	}
}

func TestBaseTxRoundTrip(t *testing.T) {
	const wantNetID uint32 = 1337
	wantChain := ids.ID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	wantMemo := []byte("LP-023 batch 2 canary memo")

	tx := NewBaseTx(wantNetID, wantChain, wantMemo)
	if !IsZAPBytes(tx.Bytes()) {
		t.Fatal("Bytes() not ZAP-formatted")
	}
	if got := tx.NetworkID(); got != wantNetID {
		t.Fatalf("NetworkID() = %d, want %d", got, wantNetID)
	}
	if got := tx.BlockchainID(); got != wantChain {
		t.Fatalf("BlockchainID() = %v, want %v", got, wantChain)
	}
	if got := tx.Memo(); !bytes.Equal(got, wantMemo) {
		t.Fatalf("Memo() = %x, want %x", got, wantMemo)
	}

	tx2, err := WrapBaseTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.NetworkID() != wantNetID || tx2.BlockchainID() != wantChain || !bytes.Equal(tx2.Memo(), wantMemo) {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestBaseTxNilMemo(t *testing.T) {
	// Memo is variable-length; nil/empty memo must round-trip cleanly.
	tx := NewBaseTx(1, ids.ID{0xfe, 0xed}, nil)
	if memo := tx.Memo(); len(memo) != 0 {
		t.Fatalf("nil-memo round-trip got %x, want empty", memo)
	}
}

func TestRegisterL1ValidatorTxRoundTrip(t *testing.T) {
	wantValID := ids.ID{0xaa, 0xbb, 0xcc, 0xdd}
	var wantBLS [bls.PublicKeyLen]byte
	for i := range wantBLS {
		wantBLS[i] = byte(i + 1)
	}
	var wantPoP [bls.SignatureLen]byte
	for i := range wantPoP {
		wantPoP[i] = byte(0xff - i)
	}
	const wantExpiry uint64 = 1_900_000_000
	wantOwnerID := ids.ID{0x12, 0x34, 0x56, 0x78}

	tx := NewRegisterL1ValidatorTx(wantValID, wantBLS, wantPoP, wantExpiry, wantOwnerID)
	if tx.ValidationID() != wantValID {
		t.Fatal("ValidationID mismatch")
	}
	if tx.BLSPublicKey() != wantBLS {
		t.Fatal("BLSPublicKey mismatch")
	}
	if tx.ProofOfPossession() != wantPoP {
		t.Fatal("ProofOfPossession mismatch")
	}
	if tx.Expiry() != wantExpiry {
		t.Fatalf("Expiry = %d, want %d", tx.Expiry(), wantExpiry)
	}
	if tx.RemainingBalanceOwnerID() != wantOwnerID {
		t.Fatal("RemainingBalanceOwnerID mismatch")
	}

	tx2, err := WrapRegisterL1ValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.ValidationID() != wantValID || tx2.BLSPublicKey() != wantBLS ||
		tx2.ProofOfPossession() != wantPoP || tx2.Expiry() != wantExpiry ||
		tx2.RemainingBalanceOwnerID() != wantOwnerID {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestSlashValidatorTxRoundTrip(t *testing.T) {
	wantNode := ids.NodeID{0x01, 0x02, 0x03, 0x04, 0x05}
	wantNet := ids.ID{0xa1, 0xa2, 0xa3, 0xa4}
	const wantPct uint32 = 250_000 // 25% in PercentDenominator units

	tx := NewSlashValidatorTx(wantNode, wantNet, wantPct)
	if tx.NodeID() != wantNode {
		t.Fatalf("NodeID = %v, want %v", tx.NodeID(), wantNode)
	}
	if tx.Network() != wantNet {
		t.Fatal("Network mismatch")
	}
	if tx.SlashPercentage() != wantPct {
		t.Fatalf("SlashPercentage = %d, want %d", tx.SlashPercentage(), wantPct)
	}

	tx2, err := WrapSlashValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.NodeID() != wantNode || tx2.Network() != wantNet || tx2.SlashPercentage() != wantPct {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestTransferChainOwnershipTxRoundTrip(t *testing.T) {
	wantChain := ids.ID{0xc0, 0xc1, 0xc2}
	const wantThreshold uint32 = 1
	const wantLocktime uint64 = 1_800_000_000
	wantAddr := ids.ShortID{0xbe, 0xef, 0xca, 0xfe}

	tx := NewTransferChainOwnershipTx(wantChain, wantThreshold, wantLocktime, wantAddr)
	if tx.Chain() != wantChain {
		t.Fatal("Chain mismatch")
	}
	if tx.OwnerThreshold() != wantThreshold {
		t.Fatalf("OwnerThreshold = %d, want %d", tx.OwnerThreshold(), wantThreshold)
	}
	if tx.OwnerLocktime() != wantLocktime {
		t.Fatalf("OwnerLocktime = %d, want %d", tx.OwnerLocktime(), wantLocktime)
	}
	if tx.OwnerAddress() != wantAddr {
		t.Fatal("OwnerAddress mismatch")
	}

	tx2, err := WrapTransferChainOwnershipTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.Chain() != wantChain || tx2.OwnerThreshold() != wantThreshold ||
		tx2.OwnerLocktime() != wantLocktime || tx2.OwnerAddress() != wantAddr {
		t.Fatal("wrap-round-trip mismatch")
	}
}

func TestRemoveChainValidatorTxRoundTrip(t *testing.T) {
	wantNode := ids.NodeID{0x10, 0x20, 0x30, 0x40, 0x50}
	wantNet := ids.ID{0xfa, 0xce, 0xb0, 0x0c}

	tx := NewRemoveChainValidatorTx(wantNode, wantNet)
	if tx.NodeID() != wantNode {
		t.Fatal("NodeID mismatch")
	}
	if tx.Network() != wantNet {
		t.Fatal("Network mismatch")
	}

	tx2, err := WrapRemoveChainValidatorTx(tx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if tx2.NodeID() != wantNode || tx2.Network() != wantNet {
		t.Fatal("wrap-round-trip mismatch")
	}
}

// Zero-allocation accessor verification across all the simple types. Reading
// a field from a wrapped accessor must allocate 0 — that's the whole point
// of native ZAP.
func TestAllAccessorsZeroAlloc(t *testing.T) {
	id := ids.ID{1, 2, 3}
	nodeID := ids.NodeID{4, 5, 6}
	shortID := ids.ShortID{7, 8, 9}
	var blsPub [bls.PublicKeyLen]byte
	var pop [bls.SignatureLen]byte

	atx := NewAdvanceTimeTx(123)
	rtx := NewRewardValidatorTx(id)
	stx := NewSetL1ValidatorWeightTx(id, 1, 1)
	itx := NewIncreaseL1ValidatorBalanceTx(id, 1)
	dtx := NewDisableL1ValidatorTx(id)
	btx := NewBaseTx(1337, id, []byte("memo"))
	regTx := NewRegisterL1ValidatorTx(id, blsPub, pop, 1, id)
	slTx := NewSlashValidatorTx(nodeID, id, 100_000)
	tcoTx := NewTransferChainOwnershipTx(id, 1, 0, shortID)
	rcvTx := NewRemoveChainValidatorTx(nodeID, id)

	cases := []struct {
		name string
		fn   func()
	}{
		{"AdvanceTimeTx.Time", func() { _ = atx.Time() }},
		{"SetL1ValidatorWeight.Nonce", func() { _ = stx.Nonce() }},
		{"SetL1ValidatorWeight.Weight", func() { _ = stx.Weight() }},
		{"IncreaseL1ValidatorBalance.Balance", func() { _ = itx.Balance() }},
		// 32-byte ID-returning accessors construct an ids.ID by-value (4 uint64
		// loads compiled inline); the by-value return places it on the stack
		// for callers. AllocsPerRun reports 0 for these on stack-able sites.
		{"RewardValidatorTx.TxID", func() { _ = rtx.TxID() }},
		{"DisableL1ValidatorTx.ValidationID", func() { _ = dtx.ValidationID() }},
		// Batch 2 accessors.
		{"BaseTx.NetworkID", func() { _ = btx.NetworkID() }},
		{"BaseTx.BlockchainID", func() { _ = btx.BlockchainID() }},
		{"BaseTx.Memo", func() { _ = btx.Memo() }},
		{"RegisterL1ValidatorTx.Expiry", func() { _ = regTx.Expiry() }},
		{"RegisterL1ValidatorTx.ValidationID", func() { _ = regTx.ValidationID() }},
		{"SlashValidatorTx.NodeID", func() { _ = slTx.NodeID() }},
		{"SlashValidatorTx.Network", func() { _ = slTx.Network() }},
		{"SlashValidatorTx.SlashPercentage", func() { _ = slTx.SlashPercentage() }},
		{"TransferChainOwnershipTx.Chain", func() { _ = tcoTx.Chain() }},
		{"TransferChainOwnershipTx.OwnerThreshold", func() { _ = tcoTx.OwnerThreshold() }},
		{"TransferChainOwnershipTx.OwnerLocktime", func() { _ = tcoTx.OwnerLocktime() }},
		{"TransferChainOwnershipTx.OwnerAddress", func() { _ = tcoTx.OwnerAddress() }},
		{"RemoveChainValidatorTx.NodeID", func() { _ = rcvTx.NodeID() }},
		{"RemoveChainValidatorTx.Network", func() { _ = rcvTx.Network() }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := testing.AllocsPerRun(100, c.fn); got != 0 {
				t.Fatalf("%s: %.2f allocs/run, want 0", c.name, got)
			}
		})
	}
}

// 192-byte and 48-byte by-value array accessors. Same stack-able-by-return
// guarantee as the 32-byte ones; this test pins it explicitly because the
// Go compiler's escape analysis is sensitive to return size.
func TestRegisterL1ValidatorTxLargeArrayAccessorsZeroAlloc(t *testing.T) {
	var blsPub [bls.PublicKeyLen]byte
	var pop [bls.SignatureLen]byte
	regTx := NewRegisterL1ValidatorTx(ids.ID{1}, blsPub, pop, 1, ids.ID{2})

	cases := []struct {
		name string
		fn   func()
	}{
		{"BLSPublicKey", func() { _ = regTx.BLSPublicKey() }},
		{"ProofOfPossession", func() { _ = regTx.ProofOfPossession() }},
		{"RemainingBalanceOwnerID", func() { _ = regTx.RemainingBalanceOwnerID() }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := testing.AllocsPerRun(100, c.fn); got != 0 {
				t.Fatalf("%s: %.2f allocs/run, want 0", c.name, got)
			}
		})
	}
}

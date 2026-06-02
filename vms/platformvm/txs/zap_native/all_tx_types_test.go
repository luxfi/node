// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

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

// Zero-allocation accessor verification across all the simple types. Reading
// a field from a wrapped accessor must allocate 0 — that's the whole point
// of native ZAP.
func TestAllAccessorsZeroAlloc(t *testing.T) {
	id := ids.ID{1, 2, 3}

	atx := NewAdvanceTimeTx(123)
	rtx := NewRewardValidatorTx(id)
	stx := NewSetL1ValidatorWeightTx(id, 1, 1)
	itx := NewIncreaseL1ValidatorBalanceTx(id, 1)
	dtx := NewDisableL1ValidatorTx(id)

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

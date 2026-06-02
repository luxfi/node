// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TestOwnerMultiAddressRoundTrip pins the multi-address Owner primitive.
// Parent tx embeds SizeOwnerHeader bytes at offset 0; address list lives in
// the variable section.
func TestOwnerMultiAddressRoundTrip(t *testing.T) {
	addrs := []ids.ShortID{
		{0x01, 0x02, 0x03},
		{0x10, 0x20, 0x30},
		{0xa0, 0xb0, 0xc0},
	}
	in := OwnerInput{
		Threshold: 2,
		Locktime:  1_900_000_000,
		Addresses: addrs,
	}

	b := zap.NewBuilder(256)
	threshold, locktime, addrOff, addrLen, err := NewOwnerInline(b, in)
	if err != nil {
		t.Fatalf("NewOwnerInline: %v", err)
	}

	ob := b.StartObject(SizeOwnerHeader)
	ob.SetUint32(OffsetOwnerHeader_Threshold, threshold)
	ob.SetUint64(OffsetOwnerHeader_Locktime, locktime)
	ob.SetList(OffsetOwnerHeader_AddressList, addrOff, addrLen)
	ob.FinishAsRoot()

	msg, err := zap.Parse(b.Finish())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	owner := OwnerView(msg.Root(), 0)
	if owner.Threshold() != 2 {
		t.Errorf("Threshold = %d, want 2", owner.Threshold())
	}
	if owner.Locktime() != 1_900_000_000 {
		t.Errorf("Locktime = %d, want 1900000000", owner.Locktime())
	}

	addrList := owner.Addresses()
	if addrList.Len() != 3 {
		t.Fatalf("AddressList.Len() = %d, want 3", addrList.Len())
	}
	for i, want := range addrs {
		got := addrList.At(i)
		if got != want {
			t.Errorf("address[%d] = %x, want %x", i, got, want)
		}
	}
}

// TestOwnerSingleAddressRefused pins the design decision: NewOwnerInline
// refuses single-address inputs so callers consciously pick OwnerStub for
// the zero-alloc fast path.
func TestOwnerSingleAddressRefused(t *testing.T) {
	in := OwnerInput{
		Threshold: 1,
		Locktime:  0,
		Addresses: []ids.ShortID{{0x42}},
	}
	b := zap.NewBuilder(64)
	_, _, _, _, err := NewOwnerInline(b, in)
	if err != ErrOwnerSingleAddrUseStub {
		t.Fatalf("NewOwnerInline(1 addr) = %v, want ErrOwnerSingleAddrUseStub", err)
	}
}

// TestOwnerEmptyAddressRefused pins zero-address inputs as also refused;
// an Owner with no addresses is logically invalid.
func TestOwnerEmptyAddressRefused(t *testing.T) {
	in := OwnerInput{Threshold: 0, Locktime: 0, Addresses: nil}
	b := zap.NewBuilder(64)
	_, _, _, _, err := NewOwnerInline(b, in)
	if err != ErrOwnerSingleAddrUseStub {
		t.Fatalf("NewOwnerInline(0 addrs) = %v, want ErrOwnerSingleAddrUseStub", err)
	}
}

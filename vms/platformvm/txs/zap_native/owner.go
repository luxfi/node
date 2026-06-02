// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// AddressList is a variable-length list of 20-byte ids.ShortID addresses.
// Stride is ids.ShortIDLen (20). The wire layer guarantees length*stride
// fits in the message buffer via Object.ListStride (zap v0.7.2+).
type AddressList struct {
	list zap.List
}

// Len returns the number of addresses.
func (a AddressList) Len() int { return a.list.Len() }

// IsNull returns true if no list pointer was set.
func (a AddressList) IsNull() bool { return a.list.IsNull() }

// At returns the i'th address. Returns the zero ShortID when out of range.
func (a AddressList) At(i int) ids.ShortID {
	var out ids.ShortID
	if i < 0 || i >= a.list.Len() {
		return out
	}
	// Read 20 bytes via the list's underlying byte indexing.
	obj := a.list.Object(i, ids.ShortIDLen)
	for j := 0; j < ids.ShortIDLen; j++ {
		out[j] = obj.Uint8(j)
	}
	return out
}

// AddressListView reads an AddressList from a parent object's field offset.
// Uses the per-stride clamp introduced in zap v0.7.2: a poisoned length
// field that passes the permissive baseline gets rejected here.
//
// READ-ONLY: each address aliases the underlying ZAP buffer.
func AddressListView(parent zap.Object, fieldOffset int) AddressList {
	return AddressList{list: parent.ListStride(fieldOffset, ids.ShortIDLen)}
}

// WriteAddressList writes an address list at the builder's current position
// and returns (offset, entryCount) suitable for ObjectBuilder.SetList.
func WriteAddressList(b *zap.Builder, addrs []ids.ShortID) (offset, entryCount int) {
	if len(addrs) == 0 {
		return 0, 0
	}
	lb := b.StartList(ids.ShortIDLen)
	for _, a := range addrs {
		lb.AddBytes(a[:])
	}
	off, _ := lb.Finish()
	return off, len(addrs)
}

// ErrOwnerSingleAddrUseStub is returned by NewOwner when the input
// addresses slice has length 1 — callers should use OwnerStub directly
// for the zero-alloc fast path. Multi-address Owner exists only when
// threshold > 1 or multiple co-signers are required.
var ErrOwnerSingleAddrUseStub = errors.New(
	"zap_native: Owner requires len(Addresses) >= 2; use OwnerStub for single-address fast path",
)

// Owner is the multi-address output owner. It composes a threshold +
// locktime + a variable AddressList. Stride for the AddressList lives in
// the same buffer's variable section; the parent object's fixed section
// carries (threshold, locktime, AddressList ptr).
//
// Wire layout per Owner header (size 16 bytes; the AddressList itself
// lives in the variable section pointed-to by the list pointer):
//
//	Threshold       uint32 @ 0
//	Locktime        uint64 @ 4
//	AddressList     8B     @ 12   (4-byte relOffset + 4-byte length)
//
// Total header = 20 bytes. The Owner header is embedded INLINE in the
// parent tx's fixed section. Variable-section addresses are appended to
// the parent's variable area by WriteOwner.
//
// Design call: OwnerStub remains the canonical single-address path
// (zero-alloc, 32 bytes total). Owner is used ONLY when len(Addresses) >= 2.
// NewOwner refuses the single-address case (ErrOwnerSingleAddrUseStub) so
// callers must consciously pick the right primitive. This forces:
//
//   - The common case stays in the fast path (OwnerStub, 32B inline).
//   - The multi-sig case carries the multi-address overhead explicitly.
//   - No silent fallback; the type system makes the choice load-bearing.
const (
	OffsetOwnerHeader_Threshold   = 0
	OffsetOwnerHeader_Locktime    = 4
	OffsetOwnerHeader_AddressList = 12
	SizeOwnerHeader               = 20
)

// Owner is the zero-copy accessor over an Owner header embedded in a parent
// tx. The AddressList view spans the parent's variable section.
//
// Owner stores the parent + base offset (not a sub-Object) because zap.Object
// is opaque to in-place sub-views; we read each field via parent.UintX at
// the (baseOffset + fieldOffset) position.
type Owner struct {
	parent     zap.Object
	baseOffset int
}

// Threshold returns the signature-required count.
func (o Owner) Threshold() uint32 {
	return o.parent.Uint32(o.baseOffset + OffsetOwnerHeader_Threshold)
}

// Locktime returns the unix timestamp before which the owner cannot spend.
func (o Owner) Locktime() uint64 {
	return o.parent.Uint64(o.baseOffset + OffsetOwnerHeader_Locktime)
}

// Addresses returns the AddressList view. Each address aliases the parent
// ZAP buffer.
func (o Owner) Addresses() AddressList {
	return AddressListView(o.parent, o.baseOffset+OffsetOwnerHeader_AddressList)
}

// OwnerView reads an Owner from a parent object's embedded-header offset.
// The Owner header is INLINE within the parent's fixed section (size
// SizeOwnerHeader = 20 bytes), so this returns a sub-view rather than
// dereferencing a relative pointer.
//
// Use case: a parent tx struct reserves SizeOwnerHeader bytes at a known
// offset for the embedded Owner; this constructor returns the accessor.
func OwnerView(parent zap.Object, fieldOffset int) Owner {
	return Owner{parent: parent, baseOffset: fieldOffset}
}

// OwnerInput is the constructor input for a multi-address Owner.
type OwnerInput struct {
	Threshold uint32
	Locktime  uint64
	Addresses []ids.ShortID
}

// NewOwnerInline writes the Owner's variable-section addresses into the
// builder and returns the (threshold, locktime, addrListOff, addrListLen)
// quad ready for the parent ObjectBuilder to write into the embedded
// Owner header. Returns ErrOwnerSingleAddrUseStub when len(Addresses) < 2.
func NewOwnerInline(b *zap.Builder, in OwnerInput) (threshold uint32, locktime uint64, addrListOff, addrListLen int, err error) {
	if len(in.Addresses) < 2 {
		return 0, 0, 0, 0, ErrOwnerSingleAddrUseStub
	}
	off, count := WriteAddressList(b, in.Addresses)
	return in.Threshold, in.Locktime, off, count, nil
}

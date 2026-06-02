// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TransferableOutput is the primitive entry of an OutputList. Each entry is a
// fixed-stride record carrying an asset identifier, an amount, a threshold,
// a locktime, and a single owner address (the v3 stub form used while the
// multi-address Owner schema is pending). Multi-address output owners go
// through the legacy codec gate until the BAddressList primitive ships.
//
// Wire layout per entry (size 96 bytes, tightly packed; uint64 reads are
// alignment-tolerant so no padding):
//
//	AssetID        32B    @ 0     (ids.ID)
//	Amount         uint64 @ 32
//	Threshold      uint32 @ 40
//	Locktime       uint64 @ 44
//	OwnerAddress   20B    @ 52    (ids.ShortID, single addr v3 stub)
//	Reserved       8B     @ 72    (reserved-zero; future Owner pointer)
//	_              16B    @ 80    (alignment slack for stride 96; reserved-zero)
//
// Stride is 96 bytes (= ceil(80 + 0) padded to 8-aligned 96). Tightly-packed
// last-field-ends-at-80 would be 80 but list strides are kept at a
// natural-aligned multiple to keep zap.List.Object(i, stride) arithmetic
// branch-free across architectures.
const (
	OffsetTransferableOutput_AssetID      = 0
	OffsetTransferableOutput_Amount       = 32 // uint64
	OffsetTransferableOutput_Threshold    = 40 // uint32
	OffsetTransferableOutput_Locktime     = 44 // uint64
	OffsetTransferableOutput_OwnerAddress = 52 // 20B
	SizeTransferableOutput                = 96 // stride (with reserved-zero tail)
)

// TransferableOutput is the zero-copy view over one entry in an OutputList.
type TransferableOutput struct {
	obj zap.Object
}

// AssetID returns the asset identifier (32B).
func (t TransferableOutput) AssetID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransferableOutput_AssetID + i)
	}
	return out
}

// Amount returns the asset amount.
func (t TransferableOutput) Amount() uint64 {
	return t.obj.Uint64(OffsetTransferableOutput_Amount)
}

// Threshold returns the owner-threshold (signatures-required count).
func (t TransferableOutput) Threshold() uint32 {
	return t.obj.Uint32(OffsetTransferableOutput_Threshold)
}

// Locktime returns the unix timestamp before which the output is spendable
// only by the locktime-clearing path.
func (t TransferableOutput) Locktime() uint64 {
	return t.obj.Uint64(OffsetTransferableOutput_Locktime)
}

// OwnerAddress returns the single owner address (v3 stub form). Multi-address
// outputs travel through the legacy codec gate.
func (t TransferableOutput) OwnerAddress() ids.ShortID {
	var out ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetTransferableOutput_OwnerAddress + i)
	}
	return out
}

// OutputList is the zero-copy view over a list of TransferableOutput entries.
type OutputList struct {
	list zap.List
}

// Len returns the number of entries.
func (l OutputList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l OutputList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th TransferableOutput. Returns a zero accessor when out
// of range.
func (l OutputList) At(i int) TransferableOutput {
	return TransferableOutput{obj: l.list.Object(i, SizeTransferableOutput)}
}

// OutputListView reads an OutputList from a parent object's field offset.
//
// READ-ONLY: each TransferableOutput aliases the underlying ZAP buffer.
// Mutation corrupts the parsed tx and any TxID = hash(buffer) computed
// downstream. To own the bytes, copy first: append([]byte(nil), buf...).
func OutputListView(parent zap.Object, fieldOffset int) OutputList {
	return OutputList{list: parent.List(fieldOffset)}
}

// OutputListEntry is the input to an OutputList builder. Constructors copy
// each field into the list-builder's stride.
type OutputListEntry struct {
	AssetID      ids.ID
	Amount       uint64
	Threshold    uint32
	Locktime     uint64
	OwnerAddress ids.ShortID
}

// WriteOutputList allocates a ZAP list at the builder's current position and
// returns (offset, entryCount) suitable for ObjectBuilder.SetList. The
// returned length is the ENTRY count (not the byte count); SetList stores
// this verbatim and List.Object(i, SizeTransferableOutput) uses it for
// stride-indexed access.
//
// Zero-alloc on the hot path: a stack-sized scratch buffer per entry holds
// the encoded bytes before AddBytes copies them into the ZAP buffer; no
// per-entry heap allocation.
func WriteOutputList(b *zap.Builder, entries []OutputListEntry) (offset, entryCount int) {
	if len(entries) == 0 {
		return 0, 0
	}
	lb := b.StartList(SizeTransferableOutput)
	for _, e := range entries {
		var entry [SizeTransferableOutput]byte
		copy(entry[OffsetTransferableOutput_AssetID:], e.AssetID[:])
		putU64(entry[OffsetTransferableOutput_Amount:], e.Amount)
		putU32(entry[OffsetTransferableOutput_Threshold:], e.Threshold)
		putU64(entry[OffsetTransferableOutput_Locktime:], e.Locktime)
		copy(entry[OffsetTransferableOutput_OwnerAddress:], e.OwnerAddress[:])
		lb.AddBytes(entry[:])
	}
	off, _ := lb.Finish()
	return off, len(entries)
}

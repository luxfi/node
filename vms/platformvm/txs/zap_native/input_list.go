// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TransferableInput is the primitive entry of an InputList. Each entry is a
// fixed-stride record carrying the spent UTXO ID, the asset identifier, the
// amount being spent, and a slice [SigIndicesStart, SigIndicesStart+
// SigIndicesCount) into a shared SigIndices uint32 array carried as a
// sibling field of the parent transaction object.
//
// Wire layout per entry (stride 88 bytes; uint64 reads are
// alignment-tolerant so no padding):
//
//	TxID              32B    @ 0     (ids.ID; UTXO.TxID)
//	OutputIndex       uint32 @ 32
//	AssetID           32B    @ 36    (ids.ID)
//	Amount            uint64 @ 68
//	SigIndicesStart   uint32 @ 76    (offset into shared SigIndices array)
//	SigIndicesCount   uint32 @ 80    (length of slice)
//	Reserved          4B     @ 84    (reserved-zero; 8-aligned stride 88)
//
// Companion field on the parent: SigIndices uint32 list of total length
// sum(input[i].SigIndicesCount). Each input's signature-index slice is
// SigIndices[Start : Start+Count].
const (
	OffsetTransferableInput_TxID            = 0
	OffsetTransferableInput_OutputIndex     = 32 // uint32
	OffsetTransferableInput_AssetID         = 36 // 32B
	OffsetTransferableInput_Amount          = 68 // uint64
	OffsetTransferableInput_SigIndicesStart = 76 // uint32 (index into shared array)
	OffsetTransferableInput_SigIndicesCount = 80 // uint32 (slice length)
	SizeTransferableInput                   = 88
)

// TransferableInput is the zero-copy view over one entry in an InputList.
//
// READ-ONLY: backed by the underlying ZAP buffer. Mutation corrupts the
// parsed tx and breaks TxID = hash(buffer). Use append([]byte(nil), ...)
// to take ownership of any returned slice.
type TransferableInput struct {
	obj zap.Object
}

// TxID returns the spent UTXO's tx id (32B).
func (t TransferableInput) TxID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransferableInput_TxID + i)
	}
	return out
}

// OutputIndex returns the spent UTXO's output index.
func (t TransferableInput) OutputIndex() uint32 {
	return t.obj.Uint32(OffsetTransferableInput_OutputIndex)
}

// AssetID returns the asset identifier.
func (t TransferableInput) AssetID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransferableInput_AssetID + i)
	}
	return out
}

// Amount returns the input amount.
func (t TransferableInput) Amount() uint64 {
	return t.obj.Uint64(OffsetTransferableInput_Amount)
}

// SigIndicesStart returns the start index into the parent tx's shared
// SigIndices uint32 array for this input's signature-index slice.
func (t TransferableInput) SigIndicesStart() uint32 {
	return t.obj.Uint32(OffsetTransferableInput_SigIndicesStart)
}

// SigIndicesCount returns the count of signature indices for this input.
func (t TransferableInput) SigIndicesCount() uint32 {
	return t.obj.Uint32(OffsetTransferableInput_SigIndicesCount)
}

// InputList is the zero-copy view over a list of TransferableInput entries.
type InputList struct {
	list zap.List
}

// Len returns the entry count.
func (l InputList) Len() int { return l.list.Len() }

// IsNull returns true if no list pointer was set.
func (l InputList) IsNull() bool { return l.list.IsNull() }

// At returns the i'th TransferableInput.
func (l InputList) At(i int) TransferableInput {
	return TransferableInput{obj: l.list.Object(i, SizeTransferableInput)}
}

// InputListView reads an InputList from a parent object's field offset.
func InputListView(parent zap.Object, fieldOffset int) InputList {
	return InputList{list: parent.List(fieldOffset)}
}

// InputListEntry is the constructor input for an InputList. SigIndices is the
// per-input signature-index slice; WriteInputList concatenates them all and
// returns a shared-array view that constructors store as a sibling field.
type InputListEntry struct {
	TxID        ids.ID
	OutputIndex uint32
	AssetID     ids.ID
	Amount      uint64
	SigIndices  []uint32
}

// WriteInputList writes an input list and emits its fixed-stride entries.
// Each entry's SigIndicesStart points into the SigIndices array returned by
// the second result; callers MUST write that array via WriteSigIndicesArray
// (see below) and store its (offset, length) in the parent tx's
// SigIndicesArrayRel/Len fields.
//
// Returns:
//   - inputListOffset / inputListCount: pass to ObjectBuilder.SetList
//   - sigIndicesAll: the concatenated uint32 array; pass to
//     WriteSigIndicesArray for sibling-list emission
func WriteInputList(b *zap.Builder, entries []InputListEntry) (
	inputListOffset, inputListCount int,
	sigIndicesAll []uint32,
) {
	if len(entries) == 0 {
		return 0, 0, nil
	}

	// Pre-compute total SigIndices count for capacity planning.
	totalSigs := 0
	for _, e := range entries {
		totalSigs += len(e.SigIndices)
	}
	sigIndicesAll = make([]uint32, 0, totalSigs)

	lb := b.StartList(SizeTransferableInput)
	cursor := uint32(0)
	for _, e := range entries {
		var entry [SizeTransferableInput]byte
		copy(entry[OffsetTransferableInput_TxID:], e.TxID[:])
		putU32(entry[OffsetTransferableInput_OutputIndex:], e.OutputIndex)
		copy(entry[OffsetTransferableInput_AssetID:], e.AssetID[:])
		putU64(entry[OffsetTransferableInput_Amount:], e.Amount)
		putU32(entry[OffsetTransferableInput_SigIndicesStart:], cursor)
		putU32(entry[OffsetTransferableInput_SigIndicesCount:], uint32(len(e.SigIndices)))
		lb.AddBytes(entry[:])

		sigIndicesAll = append(sigIndicesAll, e.SigIndices...)
		cursor += uint32(len(e.SigIndices))
	}
	off, _ := lb.Finish()
	return off, len(entries), sigIndicesAll
}

// WriteSigIndicesArray writes the concatenated uint32 sig-index array as a
// ZAP list and returns (offset, entryCount) for ObjectBuilder.SetList. Pair
// with WriteInputList above.
func WriteSigIndicesArray(b *zap.Builder, sigs []uint32) (offset, entryCount int) {
	if len(sigs) == 0 {
		return 0, 0
	}
	lb := b.StartList(4)
	for _, s := range sigs {
		lb.AddUint32(s)
	}
	off, _ := lb.Finish()
	return off, len(sigs)
}

// SigIndicesArray is the zero-copy view over the parent tx's shared uint32
// sig-index array. Each input slice is SigIndicesArray.Slice(start, count).
type SigIndicesArray struct {
	list zap.List
}

// Len returns the total number of sig indices across all inputs.
func (a SigIndicesArray) Len() int { return a.list.Len() }

// IsNull returns true if no array pointer was set.
func (a SigIndicesArray) IsNull() bool { return a.list.IsNull() }

// At returns the i'th uint32 sig index.
func (a SigIndicesArray) At(i int) uint32 { return a.list.Uint32(i) }

// SigIndicesArrayView reads the shared sig-indices array from a parent
// object's field offset.
func SigIndicesArrayView(parent zap.Object, fieldOffset int) SigIndicesArray {
	return SigIndicesArray{list: parent.List(fieldOffset)}
}

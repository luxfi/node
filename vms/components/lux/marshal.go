// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

// Native ZAP wire for the shared UTXO value tree: the struct IS the wire.
// Each type Marshal()s to a single zap object (StartObject / Set* /
// FinishAsRoot) and Unmarshal()s via offset accessors — no codec, no
// pcodecs.Manager, no serialize-tag reflection, no codec-version prefix.
//
// The polymorphic inner Out/In (fx feature-extension outputs/inputs) is
// composed, not re-encoded here: each fx primitive already owns a native
// ZAP wire envelope via its Bytes() method (and the WrapOutputBytes /
// wrapInputBytes fx-aware dispatchers reconstruct it). components/lux
// stays fx-agnostic — it hand-rolls only the outer node envelope and
// stores the fx child as an opaque bytes field. This is the same
// (TypeKind, ShapeKind, ZAP-message) envelope the fx packages hit on the
// P/X data path, so components/lux wire bytes stay consistent with the
// canonical luxfi/utxo tree without collapsing the two type trees (#58).
//
// Object fixed sections (all offsets object-relative, little-endian):
//
//	Asset              assetID 32B @0                                            size 32
//	UTXOID             txID 32B @0, index u32 @32                                size 36
//	TransferableOutput assetID 32B @0, outBytes ptr @32                          size 40
//	TransferableInput  txID 32B @0, index u32 @32, assetID 32B @36, inBytes ptr @68  size 76
//	UTXO               txID 32B @0, index u32 @32, assetID 32B @36, outBytes ptr @68 size 76
//	BaseTx             networkID u32 @0, blockchainID 32B @4, outsLen ptr @36,
//	                   outsBlob ptr @44, insLen ptr @52, insBlob ptr @60, memo ptr @68 size 76
//	Metadata           unsignedBytes ptr @0, signedBytes ptr @8                  size 16

import (
	"errors"
	"fmt"

	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

// ErrOutNotWireSerializable is returned when an in-memory Out/In is not a
// known fx primitive carrying a native ZAP Bytes() adapter.
var ErrOutNotWireSerializable = errors.New("lux: fx output/input type does not implement wire-serializable Bytes() []byte")

// wireSerializable is the minimal contract every fx primitive's wire.go
// adapter satisfies for the polymorphic child payload.
type wireSerializable interface{ Bytes() []byte }

func childBytes(v any) ([]byte, error) {
	ws, ok := v.(wireSerializable)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrOutNotWireSerializable, v)
	}
	return ws.Bytes(), nil
}

// ---- fixed offsets / sizes ----

const (
	offAssetOnly = 0
	sizeAsset    = 32

	offUTXOIDTxID  = 0
	offUTXOIDIndex = 32
	sizeUTXOID     = 36

	offTOAsset = 0
	offTOOut   = 32
	sizeTO     = 40

	offTITxID  = 0
	offTIIndex = 32
	offTIAsset = 36
	offTIIn    = 68
	sizeTI     = 76

	offUTXOTxID  = 0
	offUTXOIndex = 32
	offUTXOAsset = 36
	offUTXOOut   = 68
	sizeUTXOObj  = 76

	offBTNetworkID    = 0
	offBTBlockchainID = 4
	offBTOutsLen      = 36
	offBTOutsBlob     = 44
	offBTInsLen       = 52
	offBTInsBlob      = 60
	offBTMemo         = 68
	sizeBT            = 76

	offMDUnsigned = 0
	offMDSigned   = 8
	sizeMD        = 16

	idLen        = 32
	itemLenStrid = 4 // uint32 element for the per-item length lists
)

// ---- Asset ----

func (a *Asset) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeAsset)
	ob := b.StartObject(sizeAsset)
	ob.SetBytesFixed(offAssetOnly, a.ID[:])
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (a *Asset) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	copy(a.ID[:], msg.Root().BytesFixedSlice(offAssetOnly, idLen))
	return nil
}

// ---- UTXOID ----

func (u *UTXOID) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeUTXOID)
	ob := b.StartObject(sizeUTXOID)
	ob.SetBytesFixed(offUTXOIDTxID, u.TxID[:])
	ob.SetUint32(offUTXOIDIndex, u.OutputIndex)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (u *UTXOID) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	copy(u.TxID[:], obj.BytesFixedSlice(offUTXOIDTxID, idLen))
	u.OutputIndex = obj.Uint32(offUTXOIDIndex)
	return nil
}

// ---- TransferableOutput ----

func (out *TransferableOutput) Marshal() ([]byte, error) {
	if out == nil || out.Out == nil {
		return nil, ErrNilTransferableFxOutput
	}
	child, err := childBytes(out.Out)
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeTO + len(child) + 32)
	ob := b.StartObject(sizeTO)
	ob.SetBytesFixed(offTOAsset, out.Asset.ID[:])
	ob.SetBytes(offTOOut, child)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (out *TransferableOutput) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	copy(out.Asset.ID[:], obj.BytesFixedSlice(offTOAsset, idLen))
	fxOut, err := WrapOutputBytes(obj.Bytes(offTOOut))
	if err != nil {
		return err
	}
	to, ok := fxOut.(TransferableOut)
	if !ok {
		return fmt.Errorf("lux: decoded output %T is not a TransferableOut", fxOut)
	}
	out.Out = to
	return nil
}

// ---- TransferableInput ----

func (in *TransferableInput) Marshal() ([]byte, error) {
	if in == nil || in.In == nil {
		return nil, ErrNilTransferableFxInput
	}
	child, err := childBytes(in.In)
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeTI + len(child) + 32)
	ob := b.StartObject(sizeTI)
	ob.SetBytesFixed(offTITxID, in.UTXOID.TxID[:])
	ob.SetUint32(offTIIndex, in.UTXOID.OutputIndex)
	ob.SetBytesFixed(offTIAsset, in.Asset.ID[:])
	ob.SetBytes(offTIIn, child)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (in *TransferableInput) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	copy(in.UTXOID.TxID[:], obj.BytesFixedSlice(offTITxID, idLen))
	in.UTXOID.OutputIndex = obj.Uint32(offTIIndex)
	copy(in.Asset.ID[:], obj.BytesFixedSlice(offTIAsset, idLen))
	fxIn, err := wrapInputBytes(obj.Bytes(offTIIn))
	if err != nil {
		return err
	}
	in.In = fxIn
	return nil
}

// ---- UTXO ----

func (utxo *UTXO) Marshal() ([]byte, error) {
	if utxo == nil || utxo.Out == nil {
		return nil, errEmptyUTXO
	}
	child, err := childBytes(utxo.Out)
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeUTXOObj + len(child) + 32)
	ob := b.StartObject(sizeUTXOObj)
	ob.SetBytesFixed(offUTXOTxID, utxo.UTXOID.TxID[:])
	ob.SetUint32(offUTXOIndex, utxo.UTXOID.OutputIndex)
	ob.SetBytesFixed(offUTXOAsset, utxo.Asset.ID[:])
	ob.SetBytes(offUTXOOut, child)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (utxo *UTXO) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	copy(utxo.UTXOID.TxID[:], obj.BytesFixedSlice(offUTXOTxID, idLen))
	utxo.UTXOID.OutputIndex = obj.Uint32(offUTXOIndex)
	copy(utxo.Asset.ID[:], obj.BytesFixedSlice(offUTXOAsset, idLen))
	fxOut, err := WrapOutputBytes(obj.Bytes(offUTXOOut))
	if err != nil {
		return err
	}
	utxo.Out = fxOut
	return nil
}

// ---- BaseTx ----

func (t *BaseTx) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeBT + 256)

	outsLenOff, outsLenCount, outsBlob, err := writeOutList(b, t.Outs)
	if err != nil {
		return nil, err
	}
	insLenOff, insLenCount, insBlob, err := writeInList(b, t.Ins)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeBT)
	ob.SetUint32(offBTNetworkID, t.NetworkID)
	ob.SetBytesFixed(offBTBlockchainID, t.BlockchainID[:])
	ob.SetList(offBTOutsLen, outsLenOff, outsLenCount)
	ob.SetBytes(offBTOutsBlob, outsBlob)
	ob.SetList(offBTInsLen, insLenOff, insLenCount)
	ob.SetBytes(offBTInsBlob, insBlob)
	ob.SetBytes(offBTMemo, t.Memo)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (t *BaseTx) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	t.NetworkID = obj.Uint32(offBTNetworkID)
	copy(t.BlockchainID[:], obj.BytesFixedSlice(offBTBlockchainID, idLen))
	if t.Outs, err = readOutList(obj, offBTOutsLen, offBTOutsBlob); err != nil {
		return err
	}
	if t.Ins, err = readInList(obj, offBTInsLen, offBTInsBlob); err != nil {
		return err
	}
	if m := obj.Bytes(offBTMemo); len(m) > 0 {
		t.Memo = append([]byte(nil), m...)
	} else {
		t.Memo = nil
	}
	return nil
}

// writeOutList marshals each output and packs (per-item u32 length list,
// concatenated blob). AddUint32 counts elements, so the list length is the
// item count directly (unlike an AddBytes list, which counts bytes).
func writeOutList(b *zap.Builder, outs []*TransferableOutput) (lenOff, lenCount int, blob []byte, err error) {
	if len(outs) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(itemLenStrid)
	for i, o := range outs {
		raw, err := o.Marshal()
		if err != nil {
			return 0, 0, nil, fmt.Errorf("output %d: %w", i, err)
		}
		lb.AddUint32(uint32(len(raw)))
		blob = append(blob, raw...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob, nil
}

func writeInList(b *zap.Builder, ins []*TransferableInput) (lenOff, lenCount int, blob []byte, err error) {
	if len(ins) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(itemLenStrid)
	for i, in := range ins {
		raw, err := in.Marshal()
		if err != nil {
			return 0, 0, nil, fmt.Errorf("input %d: %w", i, err)
		}
		lb.AddUint32(uint32(len(raw)))
		blob = append(blob, raw...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob, nil
}

func readOutList(obj zap.Object, lenPtrOff, blobPtrOff int) ([]*TransferableOutput, error) {
	lengths := obj.ListStride(lenPtrOff, itemLenStrid)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(blobPtrOff)
	out := make([]*TransferableOutput, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if size < 0 || cursor+size > len(blob) {
			return nil, fmt.Errorf("lux: output %d length %d overruns blob (%d)", i, size, len(blob))
		}
		o := &TransferableOutput{}
		if err := o.Unmarshal(blob[cursor : cursor+size]); err != nil {
			return nil, fmt.Errorf("lux: unmarshal output %d: %w", i, err)
		}
		out[i] = o
		cursor += size
	}
	return out, nil
}

func readInList(obj zap.Object, lenPtrOff, blobPtrOff int) ([]*TransferableInput, error) {
	lengths := obj.ListStride(lenPtrOff, itemLenStrid)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(blobPtrOff)
	ins := make([]*TransferableInput, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if size < 0 || cursor+size > len(blob) {
			return nil, fmt.Errorf("lux: input %d length %d overruns blob (%d)", i, size, len(blob))
		}
		in := &TransferableInput{}
		if err := in.Unmarshal(blob[cursor : cursor+size]); err != nil {
			return nil, fmt.Errorf("lux: unmarshal input %d: %w", i, err)
		}
		ins[i] = in
		cursor += size
	}
	return ins, nil
}

// ---- Metadata ----

func (md *Metadata) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeMD + len(md.unsignedBytes) + len(md.bytes) + 32)
	ob := b.StartObject(sizeMD)
	ob.SetBytes(offMDUnsigned, md.unsignedBytes)
	ob.SetBytes(offMDSigned, md.bytes)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func (md *Metadata) Unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	obj := msg.Root()
	unsigned := append([]byte(nil), obj.Bytes(offMDUnsigned)...)
	signed := append([]byte(nil), obj.Bytes(offMDSigned)...)
	md.Initialize(unsigned, signed)
	return nil
}

// ---- OutputOwners path ----

// MarshalOwner is the ONE canonical byte encoding of an owner: the fx's
// own native ZAP OutputOwners envelope (TypeKindReserved,
// ShapeKindOutputOwners). Takes `any` to match the fx.Owned.Owners()
// surface. Same wire the fx TransferOutput embeds — no second encoding.
func MarshalOwner(o any) ([]byte, error) {
	return childBytes(o)
}

// UnmarshalOwner is the exact inverse of MarshalOwner: parses the
// canonical OutputOwners envelope back into *secp256k1fx.OutputOwners.
func UnmarshalOwner(b []byte) (*secp256k1fx.OutputOwners, error) {
	return secp256k1fx.WrapOutputOwners(b)
}

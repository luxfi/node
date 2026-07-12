// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Native-ZAP wire plumbing shared by every X-chain tx type: the (TypeKind,
// ShapeKind) envelope dispatchers that reconstruct polymorphic fx primitives
// (value outputs/inputs, state outputs, operations, credentials) and the
// length-prefixed blob-list codec used for the variable sections. There is no
// pcodecs.Manager and no reflect: an fx primitive names itself on the wire via
// its 2-byte discriminator, and each branch calls exactly one fx-package
// Wrap*. Adding a new (fx, shape) is a new branch — no shared codec to grow.

import (
	"fmt"

	"github.com/luxfi/ids"
	componentslux "github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/fxs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/zap"
)

// itemLenStride is the per-element width of the u32 length list that prefixes
// every packed blob list (one length per element, elements concatenated).
const itemLenStride = 4

// childBytes returns the canonical wire envelope of a wire-serializable fx
// value (every production fx primitive exposes Bytes() []byte via its wire.go
// adapter). This is the ONE serialization contract the tx wire composes on.
func childBytes(v any) ([]byte, error) {
	ws, ok := v.(interface{ Bytes() []byte })
	if !ok {
		return nil, fmt.Errorf("xvm txs: %T is not wire-serializable (missing Bytes() []byte)", v)
	}
	return ws.Bytes(), nil
}

// ---- packed blob list: (u32 length list, concatenated blob) ----

// writeBlobList packs a slice of self-contained buffers as a u32 length list
// plus a concatenated blob. AddUint32 counts elements, so the list length is
// the item count directly.
func writeBlobList(b *zap.Builder, bufs [][]byte) (lenOff, lenCount int, blob []byte) {
	if len(bufs) == 0 {
		return 0, 0, nil
	}
	lb := b.StartList(itemLenStride)
	for _, buf := range bufs {
		lb.AddUint32(uint32(len(buf)))
		blob = append(blob, buf...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob
}

// readBlobList slices the blob at blobPtrOff by the u32 lengths at lenPtrOff,
// returning each element's exact bytes (aliasing the parent buffer).
func readBlobList(obj zap.Object, lenPtrOff, blobPtrOff int) ([][]byte, error) {
	lengths := obj.ListStride(lenPtrOff, itemLenStride)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(blobPtrOff)
	out := make([][]byte, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if size < 0 || cursor+size > len(blob) {
			return nil, fmt.Errorf("xvm txs: element %d length %d overruns blob (%d)", i, size, len(blob))
		}
		out[i] = blob[cursor : cursor+size]
		cursor += size
	}
	return out, nil
}

// ---- TransferableOutput / TransferableInput <-> wire envelopes ----

// transferableOutBytes builds a wire TransferableOut envelope (AssetID + inner
// fx Output) from a lux.TransferableOutput.
func transferableOutBytes(o *lux.TransferableOutput) ([]byte, error) {
	inner, err := childBytes(o.Out)
	if err != nil {
		return nil, err
	}
	return wire.NewTransferableOut(o.Asset.ID, inner), nil
}

// transferableInBytes builds a wire TransferableIn envelope (UTXOID + AssetID +
// inner fx Input) from a lux.TransferableInput.
func transferableInBytes(in *lux.TransferableInput) ([]byte, error) {
	inner, err := childBytes(in.In)
	if err != nil {
		return nil, err
	}
	return wire.NewTransferableIn(in.UTXOID.TxID, in.UTXOID.OutputIndex, in.Asset.ID, inner), nil
}

// outputFromWire reconstructs a lux.TransferableOutput from a parsed wire
// TransferableOut, dispatching the inner fx Output on its own discriminator.
func outputFromWire(w wire.TransferableOut) (*lux.TransferableOutput, error) {
	fxOut, err := componentslux.WrapOutputBytes(w.OutputBytes())
	if err != nil {
		return nil, err
	}
	to, ok := fxOut.(lux.TransferableOut)
	if !ok {
		return nil, fmt.Errorf("xvm txs: decoded output %T is not a lux.TransferableOut", fxOut)
	}
	return &lux.TransferableOutput{Asset: lux.Asset{ID: w.AssetID()}, Out: to}, nil
}

// inputFromWire reconstructs a lux.TransferableInput from a parsed wire
// TransferableIn, dispatching the inner fx Input on its own discriminator.
func inputFromWire(w wire.TransferableIn) (*lux.TransferableInput, error) {
	fxIn, err := componentslux.WrapInputBytes(w.InputBytes())
	if err != nil {
		return nil, err
	}
	in, ok := fxIn.(lux.TransferableIn)
	if !ok {
		return nil, fmt.Errorf("xvm txs: decoded input %T is not a lux.TransferableIn", fxIn)
	}
	return &lux.TransferableInput{
		UTXOID: lux.UTXOID{TxID: w.TxID(), OutputIndex: w.OutputIndex()},
		Asset:  lux.Asset{ID: w.AssetID()},
		In:     in,
	}, nil
}

// ---- fx state output ([]verify.State in InitialState) ----

// wrapFxState reconstructs a fx state output (the polymorphic verify.State an
// InitialState carries) from its wire envelope. nftfx / propertyfx state
// shapes are dispatched here; every value-transfer fx family (secp256k1,
// mldsa, slhdsa, ed25519, secp256r1, schnorr, bls) is delegated to the shared
// components/lux output dispatcher so this package stays out of their lane.
func wrapFxState(env []byte) (verify.State, error) {
	tk, sk, err := wire.PeekDiscriminator(env)
	if err != nil {
		return nil, err
	}
	switch tk {
	case wire.TypeKindNFT:
		switch sk {
		case wire.ShapeKindNFTMintOutput:
			return nftfx.WrapMintOutput(env)
		case wire.ShapeKindNFTTransferOutput:
			return nftfx.WrapTransferOutput(env)
		}
	case wire.TypeKindProperty:
		switch sk {
		case wire.ShapeKindMintOutput:
			return propertyfx.WrapMintOutput(env)
		case wire.ShapeKindOwnedOutput:
			return propertyfx.WrapOwnedOutput(env)
		}
	}
	return componentslux.WrapOutputBytes(env)
}

// ---- fx operation (fxs.FxOperation in Operation) ----

// wrapFxOperation reconstructs the polymorphic fx operation an Operation
// carries from its wire envelope, dispatched on (TypeKind, ShapeKind).
func wrapFxOperation(env []byte) (fxs.FxOperation, error) {
	tk, sk, err := wire.PeekDiscriminator(env)
	if err != nil {
		return nil, err
	}
	switch tk {
	case wire.TypeKindSecp256k1:
		if sk == wire.ShapeKindMintOperation {
			return secp256k1fx.WrapMintOperation(env)
		}
	case wire.TypeKindNFT:
		switch sk {
		case wire.ShapeKindNFTMintOperation:
			return nftfx.WrapMintOperation(env)
		case wire.ShapeKindNFTTransferOp:
			return nftfx.WrapTransferOperation(env)
		}
	case wire.TypeKindProperty:
		switch sk {
		case wire.ShapeKindMintOperation:
			return propertyfx.WrapMintOperation(env)
		case wire.ShapeKindBurnOperation:
			return propertyfx.WrapBurnOperation(env)
		}
	}
	return nil, fmt.Errorf("xvm txs: unknown fx operation (TypeKind=0x%02x, ShapeKind=0x%02x)", tk, sk)
}

// ---- fx credential (verify.Verifiable in FxCredential) ----

// wrapFxCredential reconstructs a fx credential from its wire envelope,
// dispatched on TypeKind (every credential carries ShapeKindCredential).
func wrapFxCredential(env []byte) (verify.Verifiable, error) {
	tk, sk, err := wire.PeekDiscriminator(env)
	if err != nil {
		return nil, err
	}
	if sk != wire.ShapeKindCredential {
		return nil, fmt.Errorf("xvm txs: credential envelope has non-credential shape 0x%02x", sk)
	}
	switch tk {
	case wire.TypeKindSecp256k1:
		return secp256k1fx.WrapCredential(env)
	case wire.TypeKindNFT:
		return nftfx.WrapCredential(env)
	case wire.TypeKindProperty:
		return propertyfx.WrapCredential(env)
	}
	return nil, fmt.Errorf("xvm txs: unknown fx credential family (TypeKind=0x%02x)", tk)
}

// ---- UTXOID fixed-stride list (36 bytes: TxID 32B + OutputIndex u32) ----

const utxoIDStride = 36

// writeUTXOIDs packs []*lux.UTXOID as a fixed 36-byte-stride list. AddBytes
// counts bytes, so the caller stores len(ids) as the list count.
func writeUTXOIDs(b *zap.Builder, utxoIDs []*lux.UTXOID) (off, count int) {
	if len(utxoIDs) == 0 {
		return 0, 0
	}
	lb := b.StartList(utxoIDStride)
	for _, u := range utxoIDs {
		var e [utxoIDStride]byte
		copy(e[0:], u.TxID[:])
		e[32] = byte(u.OutputIndex)
		e[33] = byte(u.OutputIndex >> 8)
		e[34] = byte(u.OutputIndex >> 16)
		e[35] = byte(u.OutputIndex >> 24)
		lb.AddBytes(e[:])
	}
	off, _ = lb.Finish()
	return off, len(utxoIDs)
}

// readUTXOIDs reconstructs []*lux.UTXOID from a fixed 36-byte-stride list.
func readUTXOIDs(obj zap.Object, ptrOff int) []*lux.UTXOID {
	list := obj.ListStride(ptrOff, utxoIDStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	out := make([]*lux.UTXOID, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, utxoIDStride)
		var txID ids.ID
		copy(txID[:], e.BytesFixedSlice(0, 32))
		out[i] = &lux.UTXOID{TxID: txID, OutputIndex: e.Uint32(32)}
	}
	return out
}

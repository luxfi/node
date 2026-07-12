// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx             = (*ExportTx)(nil)
	_ secp256k1fx.UnsignedTx = (*ExportTx)(nil)
)

// ExportTx is a transaction that exports an asset to another blockchain.
//
// Wire (unsigned): the shared { xkind@0, baseTxEnvelope@8 } prefix, then
// DestinationChain@16 (32B) + the exported outputs as a packed (length list
// @48, blob @56) of self-delimiting wire TransferableOut envelopes.
type ExportTx struct {
	BaseTx

	// Which chain to send the funds to
	DestinationChain ids.ID `json:"destinationChain"`

	// The outputs this transaction is sending to the other chain
	ExportedOuts []*lux.TransferableOutput `json:"exportedOutputs"`
}

const (
	offExportDest     = 16 // 32B
	offExportOutsLen  = 48 // list ptr
	offExportOutsBlob = 56 // bytes ptr
	sizeExport        = 64
)

func (t *ExportTx) InitRuntime(rt *runtime.Runtime) {
	for _, out := range t.ExportedOuts {
		out.InitRuntime(rt)
	}
	t.BaseTx.InitRuntime(rt)
}

// InitializeRuntime initializes the context for this transaction
func (t *ExportTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *ExportTx) Visit(v Visitor) error {
	return v.ExportTx(t)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (t *ExportTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *ExportTx) serialize() ([]byte, error) {
	env, err := t.baseTxWire()
	if err != nil {
		return nil, err
	}
	outs := make([][]byte, len(t.ExportedOuts))
	for i, o := range t.ExportedOuts {
		b, err := transferableOutBytes(o)
		if err != nil {
			return nil, err
		}
		outs[i] = b
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeExport + len(env) + 256)
	outsLenOff, outsLenCount, outsBlob := writeBlobList(b, outs)

	ob := b.StartObject(sizeExport)
	ob.SetUint8(offXKind, uint8(xkindExport))
	ob.SetBytes(offBaseTx, env)
	ob.SetBytesFixed(offExportDest, t.DestinationChain[:])
	ob.SetList(offExportOutsLen, outsLenOff, outsLenCount)
	ob.SetBytes(offExportOutsBlob, outsBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseExportTx(unsignedBytes []byte, obj zap.Object) (*ExportTx, error) {
	base, err := decodeBaseTxWire(obj)
	if err != nil {
		return nil, err
	}
	var destChain ids.ID
	copy(destChain[:], obj.BytesFixedSlice(offExportDest, 32))
	outBufs, err := readBlobList(obj, offExportOutsLen, offExportOutsBlob)
	if err != nil {
		return nil, err
	}
	outs := make([]*lux.TransferableOutput, len(outBufs))
	for i, buf := range outBufs {
		w, err := wire.WrapTransferableOut(buf)
		if err != nil {
			return nil, err
		}
		out, err := outputFromWire(w)
		if err != nil {
			return nil, err
		}
		outs[i] = out
	}
	return &ExportTx{
		BaseTx:           BaseTx{BaseTx: base, bytes: unsignedBytes},
		DestinationChain: destChain,
		ExportedOuts:     outs,
	}, nil
}

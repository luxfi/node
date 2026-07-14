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
	offExportDest = 16 // 32B
	offExportOuts = 48 // objptr list (relOffset + count, 8 bytes)
	sizeExport    = 56
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
	b := zap.NewBuilder(zap.HeaderSize + sizeExport + len(env) + 256)
	// Append each exported TransferableOut object inline, then an objptr list —
	// native nesting, no per-output envelope prefix, no blob concat.
	offs := make([]int, len(t.ExportedOuts))
	for i, o := range t.ExportedOuts {
		inner, err := childBytes(o.Out)
		if err != nil {
			return nil, err
		}
		offs[i] = wire.AppendTransferableOut(b, o.Asset.ID, inner)
	}
	lb := b.StartList(4)
	for _, off := range offs {
		lb.AddObjectPtr(off)
	}
	outsOff, outsLen := lb.Finish()

	ob := b.StartObject(sizeExport)
	ob.SetUint8(offXKind, uint8(xkindExport))
	ob.SetBytes(offBaseTx, env)
	ob.SetBytesFixed(offExportDest, t.DestinationChain[:])
	ob.SetList(offExportOuts, outsOff, outsLen)
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
	msg := obj.Message()
	l := obj.ListStride(offExportOuts, 4)
	outs := make([]*lux.TransferableOutput, l.Len())
	for i := range outs {
		w := wire.TransferableOutFromObject(msg, l.ObjectPtr(i))
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

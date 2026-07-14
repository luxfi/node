// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx             = (*BaseTx)(nil)
	_ secp256k1fx.UnsignedTx = (*BaseTx)(nil)
)

// BaseTx is the basis of all transactions. It carries the multi-asset spending
// envelope (NetworkID/BlockchainID/Outs/Ins/Memo) as an embedded lux.BaseTx —
// X-Chain is a universal multi-fx settlement layer, so Outs/Ins hold arbitrary
// fx primitives, not the fixed secp256k1 stride the P-Chain uses. The struct
// is the source of truth; Bytes() is the cached native-ZAP wire encoding.
//
// Wire (unsigned): zap object { xkind@0, baseTxEnvelope@8 } where the envelope
// is a wire.XVMBaseTx (each Out/In an fx-typed TransferableOut/In envelope).
type BaseTx struct {
	lux.BaseTx

	bytes []byte
}

// unsigned-tx object layout shared by every X-chain tx type.
const (
	offBaseTx   = 8 // bytes ptr: the wire.XVMBaseTx envelope
	sizeBaseObj = 16
)

func (t *BaseTx) InitRuntime(rt *runtime.Runtime) {
	for _, out := range t.Outs {
		out.InitRuntime(rt)
	}
}

// InitializeRuntime initializes the context for this transaction
func (t *BaseTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *BaseTx) SetBytes(bytes []byte) {
	t.bytes = bytes
}

func (t *BaseTx) Bytes() []byte {
	return t.bytes
}

func (t *BaseTx) InputIDs() set.Set[ids.ID] {
	inputIDs := make(set.Set[ids.ID], len(t.Ins))
	for _, in := range t.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

// InputUTXOs returns the UTXOIDs this transaction is consuming
func (t *BaseTx) InputUTXOs() []*lux.UTXOID {
	utxos := make([]*lux.UTXOID, len(t.Ins))
	for i, in := range t.Ins {
		utxos[i] = &in.UTXOID
	}
	return utxos
}

func (t *BaseTx) Visit(v Visitor) error {
	return v.BaseTx(t)
}

// NumCredentials returns the number of expected credentials
func (t *BaseTx) NumCredentials() int {
	return len(t.Ins)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (t *BaseTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

// serialize encodes the unsigned tx into its canonical native-ZAP wire bytes.
func (t *BaseTx) serialize() ([]byte, error) {
	env, err := t.baseTxWire()
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeBaseObj + len(env) + 64)
	ob := b.StartObject(sizeBaseObj)
	ob.SetUint8(offXKind, uint8(xkindBase))
	ob.SetBytes(offBaseTx, env)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// baseTxWire builds the wire.XVMBaseTx envelope from the embedded spending
// envelope. Each Out/In carries its AssetID (X-Chain is multi-asset) plus the
// inner fx primitive's own wire envelope (.Bytes()); NewXVMBaseTx nests them
// natively (AddObjectPtr object lists), so there is no per-container envelope
// prefix and no blob concatenation.
func (t *BaseTx) baseTxWire() ([]byte, error) {
	outs := make([]wire.XVMTransferOut, len(t.Outs))
	for i, o := range t.Outs {
		inner, err := childBytes(o.Out)
		if err != nil {
			return nil, err
		}
		outs[i] = wire.XVMTransferOut{AssetID: o.Asset.ID, Output: inner}
	}
	ins := make([]wire.XVMTransferIn, len(t.Ins))
	for i, in := range t.Ins {
		inner, err := childBytes(in.In)
		if err != nil {
			return nil, err
		}
		ins[i] = wire.XVMTransferIn{
			TxID:        in.UTXOID.TxID,
			OutputIndex: in.UTXOID.OutputIndex,
			AssetID:     in.Asset.ID,
			Input:       inner,
		}
	}
	return wire.NewXVMBaseTx(wire.XVMBaseTxInput{
		NetworkID:    t.NetworkID,
		BlockchainID: [32]byte(t.BlockchainID),
		Outs:         outs,
		Ins:          ins,
		Memo:         t.Memo,
	}), nil
}

// decodeBaseTxWire reconstructs an embedded lux.BaseTx from the wire.XVMBaseTx
// envelope at offBaseTx of a parsed unsigned-tx object.
func decodeBaseTxWire(obj zap.Object) (lux.BaseTx, error) {
	w, err := wire.WrapXVMBaseTx(obj.Bytes(offBaseTx))
	if err != nil {
		return lux.BaseTx{}, err
	}
	base := lux.BaseTx{
		NetworkID:    w.NetworkID(),
		BlockchainID: ids.ID(w.BlockchainID()),
	}
	if n := w.OutsCount(); n > 0 {
		base.Outs = make([]*lux.TransferableOutput, n)
		for i := uint32(0); i < n; i++ {
			wo, err := w.OutAt(i)
			if err != nil {
				return lux.BaseTx{}, err
			}
			out, err := outputFromWire(wo)
			if err != nil {
				return lux.BaseTx{}, err
			}
			base.Outs[i] = out
		}
	}
	if n := w.InsCount(); n > 0 {
		base.Ins = make([]*lux.TransferableInput, n)
		for i := uint32(0); i < n; i++ {
			wi, err := w.InAt(i)
			if err != nil {
				return lux.BaseTx{}, err
			}
			in, err := inputFromWire(wi)
			if err != nil {
				return lux.BaseTx{}, err
			}
			base.Ins[i] = in
		}
	}
	if m := w.Memo(); len(m) > 0 {
		base.Memo = append([]byte(nil), m...)
	}
	return base, nil
}

// parseBaseTx wraps a bare BaseTx unsigned buffer into a typed *BaseTx.
func parseBaseTx(unsignedBytes []byte, obj zap.Object) (*BaseTx, error) {
	base, err := decodeBaseTxWire(obj)
	if err != nil {
		return nil, err
	}
	return &BaseTx{BaseTx: base, bytes: unsignedBytes}, nil
}

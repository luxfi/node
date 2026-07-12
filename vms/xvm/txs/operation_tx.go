// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx             = (*OperationTx)(nil)
	_ secp256k1fx.UnsignedTx = (*OperationTx)(nil)
)

// OperationTx is a transaction that runs a set of fx operations over existing
// UTXOs (mint / NFT transfer / property burn ...).
//
// Wire (unsigned): the shared { xkind@0, baseTxEnvelope@8 } prefix, then the
// Operations as a packed (length list @16, blob @24) of self-delimiting
// Operation objects.
type OperationTx struct {
	BaseTx

	Ops []*Operation `json:"operations"`
}

const (
	offOpsLen  = 16 // list ptr
	offOpsBlob = 24 // bytes ptr
	sizeOpTx   = 32
)

func (t *OperationTx) InitRuntime(rt *runtime.Runtime) {
	t.BaseTx.InitRuntime(rt)
}

// InitializeRuntime initializes the context for this transaction
func (t *OperationTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

// Operations track which ops this transaction is performing. The returned array
// should not be modified.
func (t *OperationTx) Operations() []*Operation {
	return t.Ops
}

func (t *OperationTx) InputUTXOs() []*lux.UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, op := range t.Ops {
		utxos = append(utxos, op.UTXOIDs...)
	}
	return utxos
}

func (t *OperationTx) InputIDs() set.Set[ids.ID] {
	inputs := t.BaseTx.InputIDs()
	for _, op := range t.Ops {
		for _, utxo := range op.UTXOIDs {
			inputs.Add(utxo.InputID())
		}
	}
	return inputs
}

// NumCredentials returns the number of expected credentials
func (t *OperationTx) NumCredentials() int {
	return t.BaseTx.NumCredentials() + len(t.Ops)
}

func (t *OperationTx) Visit(v Visitor) error {
	return v.OperationTx(t)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (t *OperationTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *OperationTx) serialize() ([]byte, error) {
	env, err := t.baseTxWire()
	if err != nil {
		return nil, err
	}
	ops := make([][]byte, len(t.Ops))
	for i, op := range t.Ops {
		b, err := op.Bytes()
		if err != nil {
			return nil, err
		}
		ops[i] = b
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeOpTx + len(env) + 256)
	opsLenOff, opsLenCount, opsBlob := writeBlobList(b, ops)

	ob := b.StartObject(sizeOpTx)
	ob.SetUint8(offXKind, uint8(xkindOperation))
	ob.SetBytes(offBaseTx, env)
	ob.SetList(offOpsLen, opsLenOff, opsLenCount)
	ob.SetBytes(offOpsBlob, opsBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseOperationTx(unsignedBytes []byte, obj zap.Object) (*OperationTx, error) {
	base, err := decodeBaseTxWire(obj)
	if err != nil {
		return nil, err
	}
	opBufs, err := readBlobList(obj, offOpsLen, offOpsBlob)
	if err != nil {
		return nil, err
	}
	ops := make([]*Operation, len(opBufs))
	for i, buf := range opBufs {
		op, err := parseOperation(buf)
		if err != nil {
			return nil, err
		}
		ops[i] = op
	}
	return &OperationTx{
		BaseTx: BaseTx{BaseTx: base, bytes: unsignedBytes},
		Ops:    ops,
	}, nil
}

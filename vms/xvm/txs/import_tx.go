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
	_ UnsignedTx             = (*ImportTx)(nil)
	_ secp256k1fx.UnsignedTx = (*ImportTx)(nil)
)

// ImportTx is a transaction that imports an asset from another blockchain.
//
// Wire (unsigned): the shared { xkind@0, baseTxEnvelope@8 } prefix, then
// SourceChain@16 (32B) + the imported inputs as a packed (length list @48,
// blob @56) of self-delimiting wire TransferableIn envelopes.
type ImportTx struct {
	BaseTx

	// Which chain to consume the funds from
	SourceChain ids.ID `json:"sourceChain"`

	// The inputs to this transaction
	ImportedIns []*lux.TransferableInput `json:"importedInputs"`
}

const (
	offImportSource  = 16 // 32B
	offImportInsLen  = 48 // list ptr
	offImportInsBlob = 56 // bytes ptr
	sizeImport       = 64
)

// InputUTXOs track which UTXOs this transaction is consuming.
func (t *ImportTx) InputUTXOs() []*lux.UTXOID {
	utxos := t.BaseTx.InputUTXOs()
	for _, in := range t.ImportedIns {
		in.Symbol = true
		utxos = append(utxos, &in.UTXOID)
	}
	return utxos
}

func (t *ImportTx) InputIDs() set.Set[ids.ID] {
	inputs := t.BaseTx.InputIDs()
	for _, in := range t.ImportedIns {
		inputs.Add(in.InputID())
	}
	return inputs
}

// NumCredentials returns the number of expected credentials
func (t *ImportTx) NumCredentials() int {
	return t.BaseTx.NumCredentials() + len(t.ImportedIns)
}

func (t *ImportTx) InitRuntime(rt *runtime.Runtime) {
	t.BaseTx.InitRuntime(rt)
}

// InitializeRuntime initializes the context for this transaction
func (t *ImportTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *ImportTx) Visit(v Visitor) error {
	return v.ImportTx(t)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (t *ImportTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *ImportTx) serialize() ([]byte, error) {
	env, err := t.baseTxWire()
	if err != nil {
		return nil, err
	}
	ins := make([][]byte, len(t.ImportedIns))
	for i, in := range t.ImportedIns {
		b, err := transferableInBytes(in)
		if err != nil {
			return nil, err
		}
		ins[i] = b
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeImport + len(env) + 256)
	insLenOff, insLenCount, insBlob := writeBlobList(b, ins)

	ob := b.StartObject(sizeImport)
	ob.SetUint8(offXKind, uint8(xkindImport))
	ob.SetBytes(offBaseTx, env)
	ob.SetBytesFixed(offImportSource, t.SourceChain[:])
	ob.SetList(offImportInsLen, insLenOff, insLenCount)
	ob.SetBytes(offImportInsBlob, insBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseImportTx(unsignedBytes []byte, obj zap.Object) (*ImportTx, error) {
	base, err := decodeBaseTxWire(obj)
	if err != nil {
		return nil, err
	}
	var sourceChain ids.ID
	copy(sourceChain[:], obj.BytesFixedSlice(offImportSource, 32))
	inBufs, err := readBlobList(obj, offImportInsLen, offImportInsBlob)
	if err != nil {
		return nil, err
	}
	ins := make([]*lux.TransferableInput, len(inBufs))
	for i, buf := range inBufs {
		w, err := wire.WrapTransferableIn(buf)
		if err != nil {
			return nil, err
		}
		in, err := inputFromWire(w)
		if err != nil {
			return nil, err
		}
		ins[i] = in
	}
	return &ImportTx{
		BaseTx:      BaseTx{BaseTx: base, bytes: unsignedBytes},
		SourceChain: sourceChain,
		ImportedIns: ins,
	}, nil
}

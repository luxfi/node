// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/runtime"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx             = (*CreateAssetTx)(nil)
	_ secp256k1fx.UnsignedTx = (*CreateAssetTx)(nil)
)

// CreateAssetTx is a transaction that creates a new asset.
//
// Wire (unsigned): the shared { xkind@0, baseTxEnvelope@8 } prefix, then
// Name@16 / Symbol@24 (text) + Denomination@32 (u8) + the InitialStates as a
// packed (length list @36, blob @44) of self-delimiting InitialState objects.
type CreateAssetTx struct {
	BaseTx
	Name         string          `json:"name"`
	Symbol       string          `json:"symbol"`
	Denomination byte            `json:"denomination"`
	States       []*InitialState `json:"initialStates"`
}

const (
	offCAName       = 16 // text ptr
	offCASymbol     = 24 // text ptr
	offCADenom      = 32 // u8
	offCAStatesLen  = 36 // list ptr
	offCAStatesBlob = 44 // bytes ptr
	sizeCA          = 52
)

func (t *CreateAssetTx) InitRuntime(rt *runtime.Runtime) {
	for _, state := range t.States {
		state.InitRuntime(rt)
	}
	t.BaseTx.InitRuntime(rt)
}

// InitializeRuntime initializes the context for this transaction
func (t *CreateAssetTx) InitializeRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

// InitialStates track which virtual machines, and the initial state of these
// machines, this asset uses. The returned array should not be modified.
func (t *CreateAssetTx) InitialStates() []*InitialState {
	return t.States
}

func (t *CreateAssetTx) Visit(v Visitor) error {
	return v.CreateAssetTx(t)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (t *CreateAssetTx) InitializeWithRuntime(rt *runtime.Runtime) error {
	t.InitRuntime(rt)
	return nil
}

func (t *CreateAssetTx) serialize() ([]byte, error) {
	env, err := t.baseTxWire()
	if err != nil {
		return nil, err
	}
	states := make([][]byte, len(t.States))
	for i, s := range t.States {
		b, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		states[i] = b
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeCA + len(env) + 256)
	statesLenOff, statesLenCount, statesBlob := writeBlobList(b, states)

	ob := b.StartObject(sizeCA)
	ob.SetUint8(offXKind, uint8(xkindCreateAsset))
	ob.SetBytes(offBaseTx, env)
	ob.SetText(offCAName, t.Name)
	ob.SetText(offCASymbol, t.Symbol)
	ob.SetUint8(offCADenom, t.Denomination)
	ob.SetList(offCAStatesLen, statesLenOff, statesLenCount)
	ob.SetBytes(offCAStatesBlob, statesBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseCreateAssetTx(unsignedBytes []byte, obj zap.Object) (*CreateAssetTx, error) {
	base, err := decodeBaseTxWire(obj)
	if err != nil {
		return nil, err
	}
	stateBufs, err := readBlobList(obj, offCAStatesLen, offCAStatesBlob)
	if err != nil {
		return nil, err
	}
	states := make([]*InitialState, len(stateBufs))
	for i, buf := range stateBufs {
		s, err := parseInitialState(buf)
		if err != nil {
			return nil, err
		}
		states[i] = s
	}
	return &CreateAssetTx{
		BaseTx:       BaseTx{BaseTx: base, bytes: unsignedBytes},
		Name:         obj.Text(offCAName),
		Symbol:       obj.Text(offCASymbol),
		Denomination: obj.Uint8(offCADenom),
		States:       states,
	}, nil
}

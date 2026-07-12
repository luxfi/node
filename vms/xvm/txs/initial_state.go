// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"cmp"
	"errors"
	"sort"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	"github.com/luxfi/zap"
)

var (
	ErrNilInitialState  = errors.New("nil initial state is not valid")
	ErrNilFxOutput      = errors.New("nil feature extension output is not valid")
	ErrOutputsNotSorted = errors.New("outputs not sorted")
	ErrUnknownFx        = errors.New("unknown feature extension")

	_ utils.Sortable[*InitialState] = (*InitialState)(nil)
)

// InitialState is the per-fx genesis state a CreateAssetTx installs: an fx
// index plus that fx's initial output set. Outs is a heterogeneous slice of fx
// state outputs, each serialized as its own (TypeKind, ShapeKind, ZAP) wire
// envelope — the FxID is recovered from the envelope's TypeKind, so it is not
// stored on the wire.
//
// Wire: zap object { FxIndex@0 (u32), Outs (length list @4, blob @12) }.
type InitialState struct {
	FxIndex uint32         `json:"fxIndex"`
	FxID    ids.ID         `json:"fxID"`
	Outs    []verify.State `json:"outputs"`
}

const (
	offISFxIndex  = 0  // u32
	offISOutsLen  = 4  // list ptr
	offISOutsBlob = 12 // bytes ptr
	sizeIS        = 20
)

func (is *InitialState) InitRuntime(*runtime.Runtime) {
	// verify.State outputs are fx-agnostic here; runtime binding happens at a
	// higher level.
}

// Bytes serializes the InitialState to its self-delimiting native-ZAP wire
// object. Each output is its fx primitive's own wire envelope.
func (is *InitialState) Bytes() ([]byte, error) {
	outs := make([][]byte, len(is.Outs))
	for i, out := range is.Outs {
		b, err := childBytes(out)
		if err != nil {
			return nil, err
		}
		outs[i] = b
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeIS + 256)
	outsLenOff, outsLenCount, outsBlob := writeBlobList(b, outs)

	ob := b.StartObject(sizeIS)
	ob.SetUint32(offISFxIndex, is.FxIndex)
	ob.SetList(offISOutsLen, outsLenOff, outsLenCount)
	ob.SetBytes(offISOutsBlob, outsBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// parseInitialState reconstructs an InitialState from its wire object, wrapping
// each output on its (TypeKind, ShapeKind) discriminator.
func parseInitialState(buf []byte) (*InitialState, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	outBufs, err := readBlobList(obj, offISOutsLen, offISOutsBlob)
	if err != nil {
		return nil, err
	}
	outs := make([]verify.State, len(outBufs))
	for i, env := range outBufs {
		out, err := wrapFxState(env)
		if err != nil {
			return nil, err
		}
		outs[i] = out
	}
	return &InitialState{
		FxIndex: obj.Uint32(offISFxIndex),
		Outs:    outs,
	}, nil
}

func (is *InitialState) Verify(numFxs int) error {
	switch {
	case is == nil:
		return ErrNilInitialState
	case is.FxIndex >= uint32(numFxs):
		return ErrUnknownFx
	}

	for _, out := range is.Outs {
		if out == nil {
			return ErrNilFxOutput
		}
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedState(is.Outs) {
		return ErrOutputsNotSorted
	}
	return nil
}

func (is *InitialState) Compare(other *InitialState) int {
	return cmp.Compare(is.FxIndex, other.FxIndex)
}

func (is *InitialState) Sort() {
	sortState(is.Outs)
}

// stateBytes returns the canonical wire bytes for a state output, used as the
// sort key. Unknown (non-wire-serializable) outputs sort as empty.
func stateBytes(v verify.State) []byte {
	b, _ := childBytes(v)
	return b
}

func sortState(vers []verify.State) {
	sort.Slice(vers, func(i, j int) bool {
		return bytes.Compare(stateBytes(vers[i]), stateBytes(vers[j])) < 0
	})
}

func isSortedState(vers []verify.State) bool {
	return sort.SliceIsSorted(vers, func(i, j int) bool {
		return bytes.Compare(stateBytes(vers[i]), stateBytes(vers[j])) < 0
	})
}

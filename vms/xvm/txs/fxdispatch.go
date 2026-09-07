// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
)

// fx dispatch — reflection-free. The set of fx primitives is a CLOSED sum type
// (a finite coproduct of statically-known variants: secp256k1fx | nftfx |
// propertyfx). Dispatch is therefore a total match on the variant tag — Go's
// type switch, which the compiler lowers to a jump on the interface type tag,
// exactly as an ADT `case` compiles. The tag then indexes a dense array. No
// reflect.TypeOf, no map[reflect.Type]int: the mechanism is compile-time-typed,
// the lookup is a single indexed load.

// numTypeKinds bounds the dense fx-family enum (wire.TypeKind values 0x00..0x09,
// rounded up). FxIndex indexes by that byte directly.
const numTypeKinds = 16

// FxIndex maps a wire.TypeKind (fx family) to its position in a parser's fx
// list. Array-backed: Get is one bounds-checked load, no hashing. A slot of -1
// means no fx of that family is registered.
type FxIndex struct {
	byKind [numTypeKinds]int
}

// NewFxIndex returns an FxIndex with every family unregistered (-1).
func NewFxIndex() *FxIndex {
	fi := &FxIndex{}
	for i := range fi.byKind {
		fi.byKind[i] = -1
	}
	return fi
}

// Set records that the fx family `kind` lives at list position `idx`.
func (fi *FxIndex) Set(kind wire.TypeKind, idx int) {
	if int(kind) < numTypeKinds {
		fi.byKind[kind] = idx
	}
}

// Get returns the fx-list index for `kind`, or (0, false) when unregistered.
func (fi *FxIndex) Get(kind wire.TypeKind) (int, bool) {
	if int(kind) >= numTypeKinds {
		return 0, false
	}
	idx := fi.byKind[kind]
	if idx < 0 {
		return 0, false
	}
	return idx, true
}

// GetFx maps a parsed fx value to its fx-list index — the reflection-free
// replacement for `reflect.TypeOf(val)` + `map[reflect.Type]int`. It reads the
// value's family via the compile-time-checked fxKindOf match, then indexes.
func (fi *FxIndex) GetFx(val interface{}) (int, bool) {
	kind, ok := fxKindOf(val)
	if !ok {
		return 0, false
	}
	return fi.Get(kind)
}

// fxKindOf returns the fx family that owns a parsed fx primitive value — a
// total match over the closed set of fx output/input/credential/operation
// types. Adding a new (fx, shape) is a new case here; the compiler checks the
// types are real. No reflection.
func fxKindOf(val interface{}) (wire.TypeKind, bool) {
	switch val.(type) {
	case *secp256k1fx.TransferInput,
		*secp256k1fx.TransferOutput,
		*secp256k1fx.MintOutput,
		*secp256k1fx.MintOperation,
		*secp256k1fx.Credential:
		return wire.TypeKindSecp256k1, true
	case *nftfx.MintOutput,
		*nftfx.TransferOutput,
		*nftfx.MintOperation,
		*nftfx.TransferOperation,
		*nftfx.Credential:
		return wire.TypeKindNFT, true
	case *propertyfx.MintOutput,
		*propertyfx.OwnedOutput,
		*propertyfx.MintOperation,
		*propertyfx.BurnOperation,
		*propertyfx.Credential:
		return wire.TypeKindProperty, true
	}
	return 0, false
}

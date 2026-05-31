// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/node/vms/platformvm/block/v0"
	"github.com/luxfi/node/vms/platformvm/txs"
)

const (
	// CodecVersionV0 is the v1.23.x ("Apricot/Banff") wire layout. It is
	// retained as a READ-ONLY decoder so that pre-codec-v1 blocks on
	// disk continue to deserialize and so that v0-derived BlockIDs and
	// TxIDs remain stable. All write paths MUST use CodecVersionV1.
	CodecVersionV0 = txs.CodecVersionV0

	// CodecVersionV1 is the current canonical block-codec wire layout.
	// Every block built by this binary is written at v1.
	CodecVersionV1 = txs.CodecVersionV1

	// CodecVersion is the canonical write version. All Marshal call
	// sites in this package use CodecVersion so that any future bump of
	// the write target updates exactly one symbol.
	CodecVersion = CodecVersionV1
)

var (
	// Codec is the v1 write/read codec.Manager. It carries ONLY the v1
	// slot map and so decodes only v1-prefixed bytes. v0 bytes must go
	// through ParseBytes (which routes to v0Codec for version 0).
	Codec codec.Manager

	// GenesisCodec is the v1 large-size codec.Manager. Same v1 slot map
	// as Codec but with an unbounded maximum size for genesis decode and
	// state-read fallback paths.
	GenesisCodec codec.Manager

	// v0Codec is the v1.23.x read-only codec.Manager. It registers v0
	// block + tx slots and unmarshals into v0.Block (the v0 interface),
	// not block.Block — these are two distinct interface destinations.
	// External packages MUST NOT Marshal at v0; that is verified at
	// Parse time (Parse never re-marshals).
	v0Codec codec.Manager

	// v0GenesisCodec mirrors v0Codec at large size.
	v0GenesisCodec codec.Manager

	// genesisLinearCodec is the underlying v1 codec for GenesisCodec.
	// This is exposed for registering additional types from other
	// packages (e.g. state) at the canonical write version. The v0
	// codecs are CLOSED to external registration — their slot maps are
	// frozen at the v1.23.x layout.
	genesisLinearCodec linearcodec.Codec
)

func init() {
	cV1 := linearcodec.NewDefault()
	gcV1 := linearcodec.NewDefault()
	cV0 := linearcodec.NewDefault()
	gcV0 := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.Codec{cV1, gcV1} {
		errs.Add(RegisterBlockTypes(c))
	}
	for _, c := range []linearcodec.Codec{cV0, gcV0} {
		errs.Add(v0.RegisterBlockTypes(c))
	}

	Codec = codec.NewDefaultManager()
	GenesisCodec = codec.NewManager(math.MaxInt32)
	v0Codec = codec.NewDefaultManager()
	v0GenesisCodec = codec.NewManager(math.MaxInt32)
	errs.Add(
		Codec.RegisterCodec(CodecVersionV1, cV1),
		GenesisCodec.RegisterCodec(CodecVersionV1, gcV1),
		v0Codec.RegisterCodec(CodecVersionV0, cV0),
		v0GenesisCodec.RegisterCodec(CodecVersionV0, gcV0),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	genesisLinearCodec = gcV1
}

// RegisterGenesisType registers a type with the GenesisCodec at the
// canonical write version (v1). This is used by other packages (e.g.
// state) to register types that are only ever encountered in genesis
// bytes. The v0 codec is closed to external registration — its slot
// map is frozen at the v1.23.x layout.
func RegisterGenesisType(val interface{}) error {
	return genesisLinearCodec.RegisterType(val)
}

// RegisterBlockTypes registers the canonical v1 block type IDs. There
// is exactly one type per block kind: ProposalBlock, AbortBlock,
// CommitBlock, StandardBlock. Tx types come from txs.RegisterTypes
// (which registers the v1 tx slot layout).
func RegisterBlockTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		txs.RegisterTypes(targetCodec),
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
	)
}

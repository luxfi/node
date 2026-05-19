// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/node/vms/platformvm/txs"
)

const CodecVersion = txs.CodecVersion

var (
	// GenesisCodec allows blocks of larger than usual size to be parsed.
	// While this gives flexibility in accommodating large genesis blocks
	// it must not be used to parse new, unverified blocks which instead
	// must be processed by Codec.
	GenesisCodec codec.Manager

	Codec codec.Manager

	// genesisLinearCodec is the underlying codec for GenesisCodec.
	// This is exposed for registering additional types from other packages.
	genesisLinearCodec linearcodec.Codec
)

func init() {
	c := linearcodec.NewDefault()
	gc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.Codec{c, gc} {
		errs.Add(RegisterBlockTypes(c))
	}

	Codec = codec.NewDefaultManager()
	GenesisCodec = codec.NewManager(math.MaxInt32)
	errs.Add(
		Codec.RegisterCodec(CodecVersion, c),
		GenesisCodec.RegisterCodec(CodecVersion, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	genesisLinearCodec = gc
}

// RegisterGenesisType registers a type with the GenesisCodec.
// This is used by other packages (e.g. state) to register types that are
// only ever encountered in genesis bytes.
func RegisterGenesisType(val interface{}) error {
	return genesisLinearCodec.RegisterType(val)
}

// RegisterBlockTypes registers the canonical block type IDs. There is exactly
// one type per block kind: StandardBlock, ProposalBlock, CommitBlock,
// AbortBlock. Tx types come from txs.RegisterTypes.
func RegisterBlockTypes(targetCodec linearcodec.Codec) error {
	return errors.Join(
		txs.RegisterTypes(targetCodec),
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
	)
}

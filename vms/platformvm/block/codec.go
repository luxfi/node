// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// CodecVersion is the sole P-Chain block-codec version — shared with the
// tx codec so a single constant pins the whole stack. ZAP-native (LE)
// per LP-023; one write path, one read path, no version dispatch.
const CodecVersion = txs.CodecVersion

var (
	// Codec is the standard-size (1 MiB) block codec.Manager. It carries
	// the single ZAP-native slot map and decodes only CodecVersion-
	// prefixed block bytes.
	Codec pcodecs.Manager

	// GenesisCodec is the unbounded-size codec.Manager used by both
	// large-genesis decode AND every P-Chain state-side read of non-block
	// byte values (feeState, L1Validator, NetToL1Conversion, fx.Owner,
	// heightRange, stateBlk). Same single-version slot map as Codec, with
	// a math.MaxInt32 size budget.
	GenesisCodec pcodecs.Manager

	// genesisLinearCodec is the underlying codec for GenesisCodec. Exposed
	// so other packages (e.g. state) can register additional types that
	// appear in genesis bytes and state-read paths at the canonical
	// version via RegisterGenesisType.
	genesisLinearCodec pcodecs.LinearCodec
)

func init() {
	c := pcodecs.NewZAPCodec()
	gc := pcodecs.NewZAPCodec()

	errs := pcodecs.Errs{}
	for _, lc := range []pcodecs.LinearCodec{c, gc} {
		errs.Add(RegisterBlockTypes(lc))
	}

	Codec = pcodecs.NewDefaultManager()
	GenesisCodec = pcodecs.NewMaxInt32Manager()
	errs.Add(
		Codec.RegisterCodec(CodecVersion, c),
		GenesisCodec.RegisterCodec(CodecVersion, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	genesisLinearCodec = gc
}

// RegisterGenesisType registers a type with the GenesisCodec so that
// values encountered in genesis bytes and state-read paths (e.g.
// legacy stateBlk) decode into the same Go type as everywhere else.
func RegisterGenesisType(val interface{}) error {
	return genesisLinearCodec.RegisterType(val)
}

// RegisterBlockTypes registers the canonical block type IDs. There is
// exactly one type per block kind: ProposalBlock, AbortBlock,
// CommitBlock, StandardBlock. Tx types come from txs.RegisterTypes
// (which registers the P-Chain tx slot layout at the same slot IDs).
func RegisterBlockTypes(targetCodec pcodecs.LinearCodec) error {
	return errors.Join(
		txs.RegisterTypes(targetCodec),
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
	)
}

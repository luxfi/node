// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
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
	// Codec is the standard-size block codec.Manager. It carries ONLY
	// the v1 slot map and so decodes only v1-prefixed block bytes via
	// codec.Manager dispatch. v0 block bytes must go through Parse,
	// which routes prefix==0 to v0Codec for the v0 Block interface.
	//
	// Block-codec dispatch is explicitly two-step (Parse extracts the
	// 2-byte prefix and selects v0Codec or Codec) because v0 blocks
	// implement v0.Block, not block.Block — they cannot be unmarshalled
	// into a block.Block destination. Hence Codec is v1-only by design.
	Codec pcodecs.Manager

	// GenesisCodec is the unbounded-size codec.Manager used by both
	// large-genesis decode AND every P-Chain state-side read of
	// non-block byte values (feeState, L1Validator, NetToL1Conversion,
	// fx.Owner, heightRange, legacy stateBlk). It registers BOTH the
	// v0 (v1.23.x Apricot/Banff) and v1 (current) tx slot maps under
	// CodecVersionV0 and CodecVersionV1 respectively, so a state read
	// of pre-codec-v1 bytes (prefix=0x0000) dispatches into the v0
	// slot map and a v1 read (prefix=0x0001) dispatches into v1.
	//
	// Writes still target CodecVersion (== CodecVersionV1) exclusively —
	// the v0 slot map is a READ-ONLY decoder. This is the same shape
	// that txs.GenesisCodec carries, which is why genesis.Codec aliases
	// txs.GenesisCodec rather than this codec.
	//
	// Note that this codec does NOT register block.Block / v0.Block
	// types directly — block parsing always goes through block.Parse,
	// not GenesisCodec.Unmarshal(b, &Block). The v0 slot map registered
	// here covers the tx + sub-tx slots referenced by serialized state
	// values (e.g. fx.Owner -> secp256k1fx.OutputOwners).
	GenesisCodec pcodecs.Manager

	// v0Codec is the v1.23.x read-only codec.Manager. It registers v0
	// block + tx slots and unmarshals into v0.Block (the v0 interface),
	// not block.Block — these are two distinct interface destinations.
	// External packages MUST NOT Marshal at v0; that is verified at
	// Parse time (Parse never re-marshals).
	v0Codec pcodecs.Manager

	// v0GenesisCodec mirrors v0Codec at large size.
	v0GenesisCodec pcodecs.Manager

	// genesisLinearCodec is the underlying v1 codec for GenesisCodec.
	// This is exposed for registering additional types from other
	// packages (e.g. state) at the canonical write version.
	genesisLinearCodec pcodecs.LinearCodec

	// genesisLinearCodecV0 is the underlying v0 codec for GenesisCodec.
	// Exposed so state-side types that want to be decodable from both
	// v0 and v1 state bytes can register symmetrically via
	// RegisterGenesisType.
	genesisLinearCodecV0 pcodecs.LinearCodec
)

func init() {
	cV1 := pcodecs.NewLinearCodec()
	gcV1 := pcodecs.NewLinearCodec()
	gcV0 := pcodecs.NewLinearCodec()
	cV0 := pcodecs.NewLinearCodec()
	gcV0BlockOnly := pcodecs.NewLinearCodec()

	errs := pcodecs.Errs{}
	for _, c := range []pcodecs.LinearCodec{cV1, gcV1} {
		errs.Add(RegisterBlockTypes(c))
	}
	// gcV0 carries only the v0 tx slot map (no block types) because
	// GenesisCodec is the state-read codec, not a block-decode codec.
	// Block parsing uses v0Codec / v0GenesisCodec (below).
	errs.Add(v0.RegisterTxTypes(gcV0))
	// cV0 and gcV0BlockOnly carry the full v0 block+tx slot map for
	// block.Parse's v0 dispatch path.
	for _, c := range []pcodecs.LinearCodec{cV0, gcV0BlockOnly} {
		errs.Add(v0.RegisterBlockTypes(c))
	}

	Codec = pcodecs.NewDefaultManager()
	GenesisCodec = pcodecs.NewMaxInt32Manager()
	v0Codec = pcodecs.NewDefaultManager()
	v0GenesisCodec = pcodecs.NewMaxInt32Manager()
	errs.Add(
		Codec.RegisterCodec(CodecVersionV1, cV1),
		// GenesisCodec carries BOTH v0 (read-only) AND v1 (read+write)
		// slot maps so state-side reads of pre-codec-v1 bytes (mainnet,
		// testnet bootstrapped at v1.23.x) succeed via Manager.Unmarshal
		// dispatch on the 2-byte wire prefix. Writes still target
		// CodecVersion (== v1).
		GenesisCodec.RegisterCodec(CodecVersionV0, gcV0),
		GenesisCodec.RegisterCodec(CodecVersionV1, gcV1),
		v0Codec.RegisterCodec(CodecVersionV0, cV0),
		v0GenesisCodec.RegisterCodec(CodecVersionV0, gcV0BlockOnly),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	genesisLinearCodec = gcV1
	genesisLinearCodecV0 = gcV0
}

// RegisterGenesisType registers a type with the GenesisCodec at BOTH
// the v0 and v1 slot positions. Used by other packages (e.g. state) to
// register types that are encountered in genesis bytes and state-read
// fallback paths. Registering at both versions keeps the slot ID
// identical across the codec.Manager dispatch so a v0-prefixed read of
// a state value (e.g. legacy stateBlk) lands on the same Go type as a
// v1-prefixed read.
//
// All registrations must succeed atomically: if the v0 registration
// fails (e.g. duplicate type) the v1 registration is still attempted so
// the underlying error is surfaced rather than silently leaving the
// codecs in a divergent state.
func RegisterGenesisType(val interface{}) error {
	v0Err := genesisLinearCodecV0.RegisterType(val)
	v1Err := genesisLinearCodec.RegisterType(val)
	return errors.Join(v0Err, v1Err)
}

// RegisterBlockTypes registers the canonical v1 block type IDs. There
// is exactly one type per block kind: ProposalBlock, AbortBlock,
// CommitBlock, StandardBlock. Tx types come from txs.RegisterTypes
// (which registers the v1 tx slot layout).
func RegisterBlockTypes(targetCodec pcodecs.LinearCodec) error {
	return errors.Join(
		txs.RegisterTypes(targetCodec),
		targetCodec.RegisterType(&ProposalBlock{}),
		targetCodec.RegisterType(&AbortBlock{}),
		targetCodec.RegisterType(&CommitBlock{}),
		targetCodec.RegisterType(&StandardBlock{}),
	)
}

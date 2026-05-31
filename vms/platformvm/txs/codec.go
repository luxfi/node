// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/utxo/secp256k1fx"
)

const (
	// CodecVersionV0 is the v1.23.x ("Apricot/Banff") wire layout. It is
	// retained as a READ-ONLY decoder so that pre-codec-v1 blocks and txs
	// on disk (mainnet, testnet) continue to deserialize. All write paths
	// MUST use CodecVersionV1.
	CodecVersionV0 uint16 = 0

	// CodecVersionV1 is the current canonical wire layout used for every
	// new tx and every new block. It is the only version produced by the
	// build/sign paths.
	CodecVersionV1 uint16 = 1

	// CodecVersion is the canonical write version. All Marshal call sites
	// in this package use CodecVersion so that any future bump of the
	// write target updates exactly one symbol.
	CodecVersion = CodecVersionV1

	// Version is retained as a deprecated alias for code that referenced
	// the pre-multi-version constant.
	Version = CodecVersion
)

var (
	// Codec is the standard-size multi-version codec used for normal txs.
	Codec codec.Manager

	// GenesisCodec allows txs of larger than usual size to be parsed.
	// It registers the same versioned slot layouts as Codec but with an
	// unbounded maximum size. New, unverified txs MUST be processed by
	// Codec; GenesisCodec is reserved for genesis decode + state read
	// fallback paths.
	GenesisCodec codec.Manager
)

func init() {
	cV0 := linearcodec.NewDefault()
	cV1 := linearcodec.NewDefault()
	gcV0 := linearcodec.NewDefault()
	gcV1 := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		registerV0TxTypes(cV0),
		registerV0TxTypes(gcV0),
		registerV1TxTypes(cV1),
		registerV1TxTypes(gcV1),
	)

	Codec = codec.NewDefaultManager()
	GenesisCodec = codec.NewManager(math.MaxInt32)
	errs.Add(
		Codec.RegisterCodec(CodecVersionV0, cV0),
		Codec.RegisterCodec(CodecVersionV1, cV1),
		GenesisCodec.RegisterCodec(CodecVersionV0, gcV0),
		GenesisCodec.RegisterCodec(CodecVersionV1, gcV1),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// RegisterTypes registers the v1 tx-codec types (the only version a
// new build path uses) on the given linearcodec. Existing call sites
// outside this package continue to compose with RegisterTypes; the v0
// decoder is gated to the txs.Codec / txs.GenesisCodec entries.
func RegisterTypes(targetCodec linearcodec.Codec) error {
	return registerV1TxTypes(targetCodec)
}

// registerV1TxTypes registers the v1 (current) slot layout. Slot map:
//
//	0-4   reserved for block codec (Skip(5))
//	5-11  secp256k1fx (TransferInput, MintOutput, TransferOutput,
//	      MintOperation, Credential, Input, OutputOwners)
//	12-20 Apricot txs
//	21-22 stakeable.{LockIn, LockOut}
//	23-26 reserved for block codec (Skip(4))
//	27-30 Banff txs
//	31-32 signer.{Empty, ProofOfPossession}
//	33-34 Durango (TransferChainOwnershipTx, BaseTx)
//	35-43 Etna + post-Etna (ConvertNetworkToL1Tx,
//	      CreateSovereignL1Tx, RegisterL1ValidatorTx,
//	      SetL1ValidatorWeightTx, IncreaseL1ValidatorBalanceTx,
//	      DisableL1ValidatorTx, SlashValidatorTx, CreateAssetTx,
//	      OperationTx)
func registerV1TxTypes(targetCodec linearcodec.Codec) error {
	// Reserve 5 slots for the four canonical block types + one historical
	// slot (atomic block ID) so existing tx type IDs remain stable.
	targetCodec.SkipRegistrations(5)

	errs := wrappers.Errs{}

	// secp256k1fx types are registered here because this matches the
	// XVM registration order — utxos crossed in shared memory must
	// have identical codec IDs across chains.
	errs.Add(
		targetCodec.RegisterType(&secp256k1fx.TransferInput{}),
		targetCodec.RegisterType(&secp256k1fx.MintOutput{}),
		targetCodec.RegisterType(&secp256k1fx.TransferOutput{}),
		targetCodec.RegisterType(&secp256k1fx.MintOperation{}),

		targetCodec.RegisterType(&secp256k1fx.Credential{}),
		targetCodec.RegisterType(&secp256k1fx.Input{}),
		targetCodec.RegisterType(&secp256k1fx.OutputOwners{}),

		// Canonical tx kinds (block-1 era).
		targetCodec.RegisterType(&AddValidatorTx{}),
		targetCodec.RegisterType(&AddChainValidatorTx{}),
		targetCodec.RegisterType(&AddDelegatorTx{}),
		targetCodec.RegisterType(&CreateNetworkTx{}),
		targetCodec.RegisterType(&CreateChainTx{}),
		targetCodec.RegisterType(&ImportTx{}),
		targetCodec.RegisterType(&ExportTx{}),
		targetCodec.RegisterType(&AdvanceTimeTx{}),
		targetCodec.RegisterType(&RewardValidatorTx{}),

		targetCodec.RegisterType(&stakeable.LockIn{}),
		targetCodec.RegisterType(&stakeable.LockOut{}),
	)

	// Skip 4 historical slots so subsequent types keep their ID.
	targetCodec.SkipRegistrations(4)

	errs.Add(
		targetCodec.RegisterType(&RemoveChainValidatorTx{}),
		targetCodec.RegisterType(&TransformChainTx{}),
		targetCodec.RegisterType(&AddPermissionlessValidatorTx{}),
		targetCodec.RegisterType(&AddPermissionlessDelegatorTx{}),

		targetCodec.RegisterType(&signer.Empty{}),
		targetCodec.RegisterType(&signer.ProofOfPossession{}),

		targetCodec.RegisterType(&TransferChainOwnershipTx{}),
		targetCodec.RegisterType(&BaseTx{}),

		targetCodec.RegisterType(&ConvertNetworkToL1Tx{}),
		// CreateSovereignL1Tx is the single-step alternative to
		// CreateNetworkTx + AddChainValidatorTx ×N + CreateChainTx ×K
		// + ConvertNetworkToL1Tx — registers a sovereign L1 atomically.
		targetCodec.RegisterType(&CreateSovereignL1Tx{}),
		targetCodec.RegisterType(&RegisterL1ValidatorTx{}),
		targetCodec.RegisterType(&SetL1ValidatorWeightTx{}),
		targetCodec.RegisterType(&IncreaseL1ValidatorBalanceTx{}),
		targetCodec.RegisterType(&DisableL1ValidatorTx{}),

		targetCodec.RegisterType(&SlashValidatorTx{}),

		// P-only primary network — assets that historically lived on the
		// X-Chain are first-class P-Chain UTXO operations.
		targetCodec.RegisterType(&CreateAssetTx{}),
		targetCodec.RegisterType(&OperationTx{}),
	)

	return errors.Join(errs.Err)
}

// registerV0TxTypes registers the v1.23.x ("Apricot/Banff") tx-codec
// slot layout. Wire-format-identical to the layout produced by
// luxfi/node@v1.23.31. v0 is registered as a READ-ONLY decoder; the
// write path (Marshal at CodecVersionV0) is never invoked from inside
// this package, and downstream code must not Marshal at CodecVersionV0.
//
// Slot map (tx-only codec):
//
//	0-4   reserved for block codec (Skip(5))
//	5     TransferInput
//	6     [skip — was secp256k1fx.MintOutput in XVM; never used on P]
//	7     TransferOutput
//	8     [skip — was secp256k1fx.MintOperation in XVM; never used on P]
//	9-11  Credential, Input, OutputOwners
//	12-20 Apricot txs
//	21-22 stakeable.{LockIn, LockOut}
//	23-26 Banff txs (RemoveChainValidator..AddPermissionlessDelegator)
//	27-28 signer.{Empty, ProofOfPossession}
//	29-32 reserved for Banff block slots (Skip(4))
//	33-34 Durango (TransferChainOwnershipTx, BaseTx)
//	35-39 Etna (ConvertChainToL1Tx [decoded as ConvertNetworkToL1Tx;
//	      same struct layout, name-only change],
//	      RegisterL1ValidatorTx, SetL1ValidatorWeightTx,
//	      IncreaseL1ValidatorBalanceTx, DisableL1ValidatorTx)
//
// Pre-Etna v0 blobs that contain only slots 5..28 decode cleanly
// without touching the post-Etna types.
func registerV0TxTypes(targetCodec linearcodec.Codec) error {
	// Slots 0-4: reserved for block-codec block types.
	targetCodec.SkipRegistrations(5)

	errs := wrappers.Errs{}

	// Slots 5-11: secp256k1fx with the two v0 historical holes.
	errs.Add(targetCodec.RegisterType(&secp256k1fx.TransferInput{}))
	targetCodec.SkipRegistrations(1) // slot 6 hole.
	errs.Add(targetCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	targetCodec.SkipRegistrations(1) // slot 8 hole.
	errs.Add(
		targetCodec.RegisterType(&secp256k1fx.Credential{}),
		targetCodec.RegisterType(&secp256k1fx.Input{}),
		targetCodec.RegisterType(&secp256k1fx.OutputOwners{}),
	)

	// Slots 12-22: Apricot txs + stakeable locks.
	errs.Add(
		targetCodec.RegisterType(&AddValidatorTx{}),
		targetCodec.RegisterType(&AddChainValidatorTx{}),
		targetCodec.RegisterType(&AddDelegatorTx{}),
		targetCodec.RegisterType(&CreateNetworkTx{}),
		targetCodec.RegisterType(&CreateChainTx{}),
		targetCodec.RegisterType(&ImportTx{}),
		targetCodec.RegisterType(&ExportTx{}),
		targetCodec.RegisterType(&AdvanceTimeTx{}),
		targetCodec.RegisterType(&RewardValidatorTx{}),
		targetCodec.RegisterType(&stakeable.LockIn{}),
		targetCodec.RegisterType(&stakeable.LockOut{}),
	)

	// Slots 23-28: Banff txs + signer.
	errs.Add(
		targetCodec.RegisterType(&RemoveChainValidatorTx{}),
		targetCodec.RegisterType(&TransformChainTx{}),
		targetCodec.RegisterType(&AddPermissionlessValidatorTx{}),
		targetCodec.RegisterType(&AddPermissionlessDelegatorTx{}),
		targetCodec.RegisterType(&signer.Empty{}),
		targetCodec.RegisterType(&signer.ProofOfPossession{}),
	)

	// Slots 29-32: reserved for Banff block slots.
	targetCodec.SkipRegistrations(4)

	// Slots 33-39: Durango + Etna. Slot 35 in v0 was named
	// ConvertChainToL1Tx; its struct layout matches the post-rename
	// ConvertNetworkToL1Tx byte-for-byte, so the same Go type is
	// registered at both v0 slot 35 and v1 slot 35.
	errs.Add(
		targetCodec.RegisterType(&TransferChainOwnershipTx{}),
		targetCodec.RegisterType(&BaseTx{}),
		targetCodec.RegisterType(&ConvertNetworkToL1Tx{}),
		targetCodec.RegisterType(&RegisterL1ValidatorTx{}),
		targetCodec.RegisterType(&SetL1ValidatorWeightTx{}),
		targetCodec.RegisterType(&IncreaseL1ValidatorBalanceTx{}),
		targetCodec.RegisterType(&DisableL1ValidatorTx{}),
	)

	return errors.Join(errs.Err)
}

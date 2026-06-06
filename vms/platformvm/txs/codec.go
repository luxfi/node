// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/utxo/secp256k1fx"
)

const (
	// CodecVersionV0 is the v1.23.x ("Apricot/Banff") wire layout. It is
	// retained as a READ-ONLY decoder so that pre-codec-v1 blocks and txs
	// on disk (mainnet, testnet) continue to deserialize. All write paths
	// at ts < ZAPCodecActivationTimestamp MUST use CodecVersionV1.
	CodecVersionV0 uint16 = 0

	// CodecVersionV1 is the canonical linearcodec layout — big-endian,
	// produced by every binary up to and including the pre-ZAP cutover.
	// Read forever; written only when block.Timestamp <
	// ZAPCodecActivationTimestamp.
	CodecVersionV1 uint16 = 1

	// CodecVersionV2 is the ZAP-native layout — little-endian, structurally
	// identical to V1 (same slot map, same field order), only the wire
	// encoding differs. Produced when block.Timestamp >=
	// ZAPCodecActivationTimestamp. Read always.
	//
	// V2 is NOT a slot-map change; it's a wire-encoding change. The same
	// Go types are registered at the same slot IDs as V1. A V1-encoded
	// tx and a V2-encoded tx of the same logical content differ in
	// byte content (LE vs BE) but produce identical Go values after
	// Unmarshal.
	CodecVersionV2 uint16 = 2

	// CodecVersion is the deprecated alias preserved for code that
	// historically referenced "the write version". New code MUST use
	// CodecVersionForTimestamp instead — there is no single canonical
	// write version after the ZAP cutover.
	CodecVersion = CodecVersionV1

	// Version is retained as a deprecated alias for code that referenced
	// the pre-multi-version constant.
	Version = CodecVersion
)

var (
	// Codec is the standard-size multi-version codec used for normal txs.
	// Registers V0 + V1 (linearcodec) AND V2 (zapcodec). The right write
	// version is selected via CodecVersionForTimestamp; reads dispatch on
	// the 2-byte wire prefix.
	Codec pcodecs.Manager

	// GenesisCodec allows txs of larger than usual size to be parsed.
	// It registers the same versioned slot layouts as Codec but with an
	// unbounded maximum size. New, unverified txs MUST be processed by
	// Codec; GenesisCodec is reserved for genesis decode + state read
	// fallback paths.
	GenesisCodec pcodecs.Manager
)

func init() {
	cV0 := pcodecs.NewLinearCodec()
	cV1 := pcodecs.NewLinearCodec()
	cV2 := pcodecs.NewZAPCodec()
	gcV0 := pcodecs.NewLinearCodec()
	gcV1 := pcodecs.NewLinearCodec()
	gcV2 := pcodecs.NewZAPCodec()

	errs := pcodecs.Errs{}
	errs.Add(
		registerV0TxTypes(cV0),
		registerV0TxTypes(gcV0),
		registerV1TxTypes(cV1),
		registerV1TxTypes(gcV1),
		// V2 reuses the V1 slot map verbatim — only the wire encoding
		// differs. Same Go types, same IDs.
		registerV1TxTypes(cV2),
		registerV1TxTypes(gcV2),
	)

	Codec = pcodecs.NewDefaultManager()
	GenesisCodec = pcodecs.NewMaxInt32Manager()
	errs.Add(
		Codec.RegisterCodec(CodecVersionV0, cV0),
		Codec.RegisterCodec(CodecVersionV1, cV1),
		Codec.RegisterCodec(CodecVersionV2, cV2),
		GenesisCodec.RegisterCodec(CodecVersionV0, gcV0),
		GenesisCodec.RegisterCodec(CodecVersionV1, gcV1),
		GenesisCodec.RegisterCodec(CodecVersionV2, gcV2),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// slotRegistrar is the subset of {linearcodec.Codec, zapcodec.Codec}
// that registerV0TxTypes and registerV1TxTypes need. Identifying it as
// an in-package interface keeps the registration functions wire-
// agnostic: they decide the SLOT MAP, the codec implementation decides
// the WIRE FORMAT. Same slot IDs across all codec implementations.
type slotRegistrar interface {
	RegisterType(interface{}) error
	SkipRegistrations(int)
}

// CodecVersionForTimestamp returns the canonical write-codec version
// for a transaction (or block) whose timestamp is ts. Reads should NOT
// use this — dispatch on the wire prefix via Codec.Unmarshal directly.
//
// The cutover is strict — a tx with ts == ZAPCodecActivationTimestamp
// is V2. A tx with ts == ZAPCodecActivationTimestamp - 1 is V1. There
// is no fallback; if the wire bytes do not match the expected version
// for the chain's current time, the tx is rejected by the codec
// version-not-allowed gate in TxsForTimestamp.
func CodecVersionForTimestamp(ts uint64) uint16 {
	if ts >= ZAPCodecActivationTimestamp {
		return CodecVersionV2
	}
	return CodecVersionV1
}

// CodecForTimestamp returns the codec.Manager configured to write at
// the canonical version for the given timestamp. The returned Manager
// is always the package-level Codec — it hosts every registered
// version. The Manager's Marshal(version, ...) method is what selects
// the wire encoding; this helper just returns the right version too,
// via the companion CodecVersionForTimestamp.
//
// Why expose both Manager AND version: callers historically pass a
// version into Codec.Marshal/Codec.Unmarshal. They keep that pattern;
// CodecForTimestamp is a no-op for the manager handle, kept here so
// future evolutions can swap the manager itself (e.g. a ZAP-only
// manager after V1 retirement) without changing call sites.
func CodecForTimestamp(_ uint64) pcodecs.Manager {
	return Codec
}

// CodecAllowsRead reports whether v is one of the codec versions this
// binary recognises for reads. Used by tx-parse gates: a wire whose
// prefix is V0/V1/V2 is accepted; anything else is rejected before
// the codec.Manager would surface ErrUnknownVersion.
//
// V0 reads are always allowed (legacy historical txs).
// V1 reads are always allowed.
// V2 reads are always allowed once the binary ships with this
//     constant — there is no "too early" rejection. The activation gate
//     only protects WRITES.
func CodecAllowsRead(v uint16) bool {
	return v == CodecVersionV0 || v == CodecVersionV1 || v == CodecVersionV2
}

// CodecRequiresLegacy reports whether wire-version v is in the
// pre-ZAP-activation legacy set. After activation a node MAY still
// accept a V1-encoded tx — there is no historical-bytes lookback
// problem — but the build path MUST refuse to PRODUCE V1 once ts
// crosses the activation threshold. This predicate distinguishes
// "legacy but accepted" from "current write version".
func CodecRequiresLegacy(v uint16) bool {
	return v == CodecVersionV0 || v == CodecVersionV1
}

// RegisterTypes registers the v1 tx-codec types (the only version a
// new build path uses) on the given linearcodec. Existing call sites
// outside this package continue to compose with RegisterTypes; the v0
// decoder is gated to the txs.Codec / txs.GenesisCodec entries.
func RegisterTypes(targetCodec pcodecs.LinearCodec) error {
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
func registerV1TxTypes(targetCodec slotRegistrar) error {
	// Reserve 5 slots for the four canonical block types + one historical
	// slot (atomic block ID) so existing tx type IDs remain stable.
	targetCodec.SkipRegistrations(5)

	errs := pcodecs.Errs{}

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
func registerV0TxTypes(targetCodec slotRegistrar) error {
	// Slots 0-4: reserved for block-codec block types.
	targetCodec.SkipRegistrations(5)

	errs := pcodecs.Errs{}

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

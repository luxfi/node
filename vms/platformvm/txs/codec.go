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

// CodecVersion is the sole P-Chain wire-codec version. The P-Chain runs
// one codec and one codec only: ZAP-native (little-endian) per LP-023.
// There is no version dispatch, no timestamp gate, and no legacy read
// path — every tx and block on this chain is (de)serialized at
// CodecVersion, and any wire whose 2-byte prefix is not CodecVersion is
// rejected by the codec.Manager (ErrUnknownVersion).
//
// Value 1: the pre-rip multi-version stack registered a legacy slot at
// version 0 (deleted) and the ZAP-native slot at version 1. The rip keeps
// the ZAP-native slot number so that no transaction ID, block ID, or
// state root produced by this codec changes — the surviving codec is
// exactly the one the chain already writes. Marshal prepends the version
// as a uint16 little-endian prefix.
const CodecVersion uint16 = 1

var (
	// Codec is the standard-size (1 MiB) P-Chain tx codec. One registered
	// version (CodecVersion), one wire format (ZAP-native LE), one write
	// path, one read path.
	Codec pcodecs.Manager

	// GenesisCodec parses txs larger than the standard max size. Same
	// single-version slot map as Codec, unbounded size budget. New,
	// unverified txs MUST be processed by Codec; GenesisCodec is reserved
	// for genesis decode + state-read paths.
	GenesisCodec pcodecs.Manager
)

func init() {
	c := pcodecs.NewZAPCodec()
	gc := pcodecs.NewZAPCodec()

	errs := pcodecs.Errs{}
	errs.Add(
		registerTxTypes(c),
		registerTxTypes(gc),
	)

	Codec = pcodecs.NewDefaultManager()
	GenesisCodec = pcodecs.NewMaxInt32Manager()
	errs.Add(
		Codec.RegisterCodec(CodecVersion, c),
		GenesisCodec.RegisterCodec(CodecVersion, gc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// RegisterTypes registers the P-Chain tx-codec slot map on targetCodec.
// Exposed so the block codec can register the tx slots on its own block
// codec instance at the SAME slot IDs — utxos crossing chains in shared
// memory must carry identical codec IDs across chains.
func RegisterTypes(targetCodec pcodecs.LinearCodec) error {
	return registerTxTypes(targetCodec)
}

// registerTxTypes registers the canonical P-Chain slot layout. Slot map:
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
//
// The slot map is load-bearing: the position of each RegisterType call
// is the on-wire type ID, so reordering or inserting a type is a wire
// break. New types append at the tail; retired slots become Skip.
func registerTxTypes(targetCodec pcodecs.LinearCodec) error {
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

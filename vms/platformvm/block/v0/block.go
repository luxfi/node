// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package v0 defines the v1.23.x ("Apricot/Banff") P-Chain block layout.
//
// These types EXIST ONLY FOR DECODING pre-codec-v1 blocks read from disk
// (mainnet, testnet) and from the wire during catch-up. New blocks are
// always written at codec version 1 using the canonical block types in
// the parent block package.
//
// Invariants:
//   - Wire layout is byte-for-byte identical to v1.23.31's
//     vms/platformvm/block package (slot IDs, struct layout).
//   - Each v0 block satisfies block.Block by translating its Visit call
//     to the canonical v1 visitor method (ApricotProposalBlock ->
//     StandardBlock at proposal-tail position, etc.).
//   - Block bytes are byte-preserved (BlockID = hash(input bytes); the
//     input bytes are stashed on CommonBlock and returned verbatim by
//     Bytes()). The block is never re-marshaled.
//   - Embedded txs are byte-preserved via tx.InitializeFromBytes using
//     the codec version their bytes were decoded under.
package v0

import (
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/utxo/secp256k1fx"
)

// CodecVersion is the wire-version prefix for the v1.23.x layout.
const CodecVersion uint16 = 0

// RegisterBlockTypes registers the v0 block + tx type IDs in their
// canonical v1.23.x order on the given linearcodec.
//
// Slot map (block codec):
//
//	0-4   Apricot blocks (Proposal, Abort, Commit, Standard, Atomic)
//	5     secp256k1fx.TransferInput
//	6     [skipped — was MintOutput, never used on P-Chain]
//	7     secp256k1fx.TransferOutput
//	8     [skipped — was MintOperation, never used on P-Chain]
//	9-11  secp256k1fx.{Credential, Input, OutputOwners}
//	12-20 Apricot txs (AddValidator..RewardValidator)
//	21-22 stakeable.{LockIn, LockOut}
//	23-26 Banff txs (RemoveChainValidator..AddPermissionlessDelegator)
//	27-28 signer.{Empty, ProofOfPossession}
//	29-32 Banff blocks (Proposal, Abort, Commit, Standard)
//	33-34 Durango txs (TransferChainOwnership, BaseTx)
//	35-39 Etna txs (ConvertChainToL1..DisableL1Validator)
//
// ConvertChainToL1Tx is registered as the wire-format-equivalent
// ConvertNetworkToL1Tx (post-rename source type, identical struct
// layout, same slot 35). Codec wire matching is by slot ID, not type
// name; the rename did not change the serialized format.
func RegisterBlockTypes(c linearcodec.Codec) error {
	errs := wrappers.Errs{}

	// Slots 0-4: Apricot blocks.
	errs.Add(
		c.RegisterType(&ApricotProposalBlock{}),
		c.RegisterType(&ApricotAbortBlock{}),
		c.RegisterType(&ApricotCommitBlock{}),
		c.RegisterType(&ApricotStandardBlock{}),
		c.RegisterType(&ApricotAtomicBlock{}),
	)

	// Slots 5-11: secp256k1fx with two historical skips at 6 and 8.
	errs.Add(c.RegisterType(&secp256k1fx.TransferInput{}))
	c.SkipRegistrations(1)
	errs.Add(c.RegisterType(&secp256k1fx.TransferOutput{}))
	c.SkipRegistrations(1)
	errs.Add(
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
	)

	// Slots 12-20: Apricot txs.
	errs.Add(
		c.RegisterType(&txs.AddValidatorTx{}),
		c.RegisterType(&txs.AddChainValidatorTx{}),
		c.RegisterType(&txs.AddDelegatorTx{}),
		c.RegisterType(&txs.CreateNetworkTx{}),
		c.RegisterType(&txs.CreateChainTx{}),
		c.RegisterType(&txs.ImportTx{}),
		c.RegisterType(&txs.ExportTx{}),
		c.RegisterType(&txs.AdvanceTimeTx{}),
		c.RegisterType(&txs.RewardValidatorTx{}),
	)

	// Slots 21-22: stakeable locks.
	errs.Add(
		c.RegisterType(&stakeable.LockIn{}),
		c.RegisterType(&stakeable.LockOut{}),
	)

	// Slots 23-26: Banff txs.
	errs.Add(
		c.RegisterType(&txs.RemoveChainValidatorTx{}),
		c.RegisterType(&txs.TransformChainTx{}),
		c.RegisterType(&txs.AddPermissionlessValidatorTx{}),
		c.RegisterType(&txs.AddPermissionlessDelegatorTx{}),
	)

	// Slots 27-28: signer types.
	errs.Add(
		c.RegisterType(&signer.Empty{}),
		c.RegisterType(&signer.ProofOfPossession{}),
	)

	// Slots 29-32: Banff blocks.
	errs.Add(
		c.RegisterType(&BanffProposalBlock{}),
		c.RegisterType(&BanffAbortBlock{}),
		c.RegisterType(&BanffCommitBlock{}),
		c.RegisterType(&BanffStandardBlock{}),
	)

	// Slots 33-34: Durango txs.
	errs.Add(
		c.RegisterType(&txs.TransferChainOwnershipTx{}),
		c.RegisterType(&txs.BaseTx{}),
	)

	// Slots 35-39: Etna txs.
	// Slot 35 was ConvertChainToL1Tx in v0 source; renamed to
	// ConvertNetworkToL1Tx in v1.27.x with identical wire layout.
	errs.Add(
		c.RegisterType(&txs.ConvertNetworkToL1Tx{}),
		c.RegisterType(&txs.RegisterL1ValidatorTx{}),
		c.RegisterType(&txs.SetL1ValidatorWeightTx{}),
		c.RegisterType(&txs.IncreaseL1ValidatorBalanceTx{}),
		c.RegisterType(&txs.DisableL1ValidatorTx{}),
	)

	return errs.Err
}

// RegisterTxTypes registers the v0 type IDs in the tx-only ordering
// used by v1.23.x txs.init(). Apricot block slots (0-4) and Banff block
// slots (29-32) are SkipRegistrations rather than RegisterType so that
// shared tx slots line up with the block codec.
func RegisterTxTypes(c linearcodec.Codec) error {
	errs := wrappers.Errs{}

	// Slots 0-4: Apricot block slots reserved.
	c.SkipRegistrations(5)

	// Slots 5-11: secp256k1fx (with two historical skips at 6 and 8).
	errs.Add(c.RegisterType(&secp256k1fx.TransferInput{}))
	c.SkipRegistrations(1)
	errs.Add(c.RegisterType(&secp256k1fx.TransferOutput{}))
	c.SkipRegistrations(1)
	errs.Add(
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
	)

	// Slots 12-22: Apricot txs + stakeable locks.
	errs.Add(
		c.RegisterType(&txs.AddValidatorTx{}),
		c.RegisterType(&txs.AddChainValidatorTx{}),
		c.RegisterType(&txs.AddDelegatorTx{}),
		c.RegisterType(&txs.CreateNetworkTx{}),
		c.RegisterType(&txs.CreateChainTx{}),
		c.RegisterType(&txs.ImportTx{}),
		c.RegisterType(&txs.ExportTx{}),
		c.RegisterType(&txs.AdvanceTimeTx{}),
		c.RegisterType(&txs.RewardValidatorTx{}),
		c.RegisterType(&stakeable.LockIn{}),
		c.RegisterType(&stakeable.LockOut{}),
	)

	// Slots 23-28: Banff txs + signer.
	errs.Add(
		c.RegisterType(&txs.RemoveChainValidatorTx{}),
		c.RegisterType(&txs.TransformChainTx{}),
		c.RegisterType(&txs.AddPermissionlessValidatorTx{}),
		c.RegisterType(&txs.AddPermissionlessDelegatorTx{}),
		c.RegisterType(&signer.Empty{}),
		c.RegisterType(&signer.ProofOfPossession{}),
	)

	// Slots 29-32: Banff block slots reserved.
	c.SkipRegistrations(4)

	// Slots 33-39: Durango + Etna.
	errs.Add(
		c.RegisterType(&txs.TransferChainOwnershipTx{}),
		c.RegisterType(&txs.BaseTx{}),
		c.RegisterType(&txs.ConvertNetworkToL1Tx{}),
		c.RegisterType(&txs.RegisterL1ValidatorTx{}),
		c.RegisterType(&txs.SetL1ValidatorWeightTx{}),
		c.RegisterType(&txs.IncreaseL1ValidatorBalanceTx{}),
		c.RegisterType(&txs.DisableL1ValidatorTx{}),
	)

	return errs.Err
}

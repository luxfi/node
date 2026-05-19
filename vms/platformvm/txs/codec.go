// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/utxo/secp256k1fx"
)

const (
	CodecVersion = 0
	Version      = CodecVersion
)

var (
	Codec codec.Manager

	// GenesisCodec allows txs of larger than usual size to be parsed.
	// While this gives flexibility in accommodating large genesis txs
	// it must not be used to parse new, unverified txs which instead
	// must be processed by Codec.
	GenesisCodec codec.Manager
)

func init() {
	c := linearcodec.NewDefault()
	gc := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	for _, c := range []linearcodec.Codec{c, gc} {
		errs.Add(RegisterTypes(c))
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
}

// RegisterTypes registers the canonical platformvm tx codec types in their
// canonical order. The ordering is fixed (it determines on-disk type IDs);
// new tx types append at the end. SkipRegistrations reserve slots for the
// block-level types registered by the block codec so that codec IDs match
// up across packages.
func RegisterTypes(targetCodec linearcodec.Codec) error {
	// Reserve 5 slots for the four canonical block types + one historical slot
	// (atomic block ID) so existing tx type IDs remain stable.
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

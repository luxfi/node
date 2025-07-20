// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet"
)

var _ Builder = (*withOptions)(nil)

type withOptions struct {
	builder Builder
	options []wallet.Option
}

// WithOptions returns a new builder that will use the given options by default.
//
//   - [builder] is the builder that will be called to perform the underlying
//     operations.
//   - [options] will be provided to the builder in addition to the options
//     provided in the method calls.
func WithOptions(builder Builder, options ...wallet.Option) Builder {
	return &withOptions{
		builder: builder,
		options: options,
	}
}

func (w *withOptions) Context() *Context {
	return w.builder.Context()
}

func (w *withOptions) GetBalance(
	options ...wallet.Option,
) (map[ids.ID]uint64, error) {
	return w.builder.GetBalance(
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) GetImportableBalance(
	chainID ids.ID,
	options ...wallet.Option,
) (map[ids.ID]uint64, error) {
	return w.builder.GetImportableBalance(
		chainID,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewBaseTx(
	outputs []*lux.TransferableOutput,
	options ...wallet.Option,
) (*txs.BaseTx, error) {
	return w.builder.NewBaseTx(
		outputs,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...wallet.Option,
) (*txs.AddValidatorTx, error) {
	return w.builder.NewAddValidatorTx(
		vdr,
		rewardsOwner,
		shares,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...wallet.Option,
) (*txs.AddSubnetValidatorTx, error) {
	return w.builder.NewAddSubnetValidatorTx(
		vdr,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...wallet.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	return w.builder.NewRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.AddDelegatorTx, error) {
	return w.builder.NewAddDelegatorTx(
		vdr,
		rewardsOwner,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...wallet.Option,
) (*txs.CreateChainTx, error) {
	return w.builder.NewCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.CreateSubnetTx, error) {
	return w.builder.NewCreateSubnetTx(
		owner,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.TransferSubnetOwnershipTx, error) {
	return w.builder.NewTransferSubnetOwnershipTx(
		subnetID,
		owner,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewConvertSubnetToL1Tx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	validators []*txs.ConvertSubnetToL1Validator,
	options ...wallet.Option,
) (*txs.ConvertSubnetToL1Tx, error) {
	return w.builder.NewConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		validators,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewRegisterL1ValidatorTx(
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
	message []byte,
	options ...wallet.Option,
) (*txs.RegisterL1ValidatorTx, error) {
	return w.builder.NewRegisterL1ValidatorTx(
		balance,
		proofOfPossession,
		message,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewSetL1ValidatorWeightTx(
	message []byte,
	options ...wallet.Option,
) (*txs.SetL1ValidatorWeightTx, error) {
	return w.builder.NewSetL1ValidatorWeightTx(
		message,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewIncreaseL1ValidatorBalanceTx(
	validationID ids.ID,
	balance uint64,
	options ...wallet.Option,
) (*txs.IncreaseL1ValidatorBalanceTx, error) {
	return w.builder.NewIncreaseL1ValidatorBalanceTx(
		validationID,
		balance,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewDisableL1ValidatorTx(
	validationID ids.ID,
	options ...wallet.Option,
) (*txs.DisableL1ValidatorTx, error) {
	return w.builder.NewDisableL1ValidatorTx(
		validationID,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.ImportTx, error) {
	return w.builder.NewImportTx(
		sourceChainID,
		to,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewExportTx(
	chainID ids.ID,
	outputs []*lux.TransferableOutput,
	options ...wallet.Option,
) (*txs.ExportTx, error) {
	return w.builder.NewExportTx(
		chainID,
		outputs,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewTransformSubnetTx(
	subnetID ids.ID,
	assetID ids.ID,
	initialSupply uint64,
	maxSupply uint64,
	minConsumptionRate uint64,
	maxConsumptionRate uint64,
	minValidatorStake uint64,
	maxValidatorStake uint64,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
	minDelegationFee uint32,
	minDelegatorStake uint64,
	maxValidatorWeightFactor byte,
	uptimeRequirement uint32,
	options ...wallet.Option,
) (*txs.TransformSubnetTx, error) {
	return w.builder.NewTransformSubnetTx(
		subnetID,
		assetID,
		initialSupply,
		maxSupply,
		minConsumptionRate,
		maxConsumptionRate,
		minValidatorStake,
		maxValidatorStake,
		minStakeDuration,
		maxStakeDuration,
		minDelegationFee,
		minDelegatorStake,
		maxValidatorWeightFactor,
		uptimeRequirement,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...wallet.Option,
) (*txs.AddPermissionlessValidatorTx, error) {
	return w.builder.NewAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		wallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...wallet.Option,
) (*txs.AddPermissionlessDelegatorTx, error) {
	return w.builder.NewAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		wallet.UnionOptions(w.options, options)...,
	)
}

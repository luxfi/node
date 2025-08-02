// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
	pwallet "github.com/luxfi/node/v2/wallet"
	"github.com/luxfi/node/v2/wallet/chain/p/builder"

	vmsigner "github.com/luxfi/node/v2/vms/platformvm/signer"
	walletsigner "github.com/luxfi/node/v2/wallet/chain/p/signer"
)

var _ Wallet = (*withOptions)(nil)

func WithOptions(
	wallet Wallet,
	options ...pwallet.Option,
) Wallet {
	return &withOptions{
		wallet:  wallet,
		options: options,
	}
}

type withOptions struct {
	wallet  Wallet
	options []pwallet.Option
}

func (w *withOptions) Builder() builder.Builder {
	return builder.WithOptions(
		w.wallet.Builder(),
		w.options...,
	)
}

func (w *withOptions) Signer() walletsigner.Signer {
	return w.wallet.Signer()
}

func (w *withOptions) IssueBaseTx(
	outputs []*lux.TransferableOutput,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueBaseTx(
		outputs,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddValidatorTx(
		vdr,
		rewardsOwner,
		shares,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddSubnetValidatorTx(
		vdr,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddDelegatorTx(
		vdr,
		rewardsOwner,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateSubnetTx(
		owner,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueTransferSubnetOwnershipTx(
		subnetID,
		owner,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueConvertSubnetToL1Tx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	validators []*txs.ConvertSubnetToL1Validator,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		validators,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueRegisterL1ValidatorTx(
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
	message []byte,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueRegisterL1ValidatorTx(
		balance,
		proofOfPossession,
		message,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueSetL1ValidatorWeightTx(
	message []byte,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueSetL1ValidatorWeightTx(
		message,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueIncreaseL1ValidatorBalanceTx(
	validationID ids.ID,
	balance uint64,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueIncreaseL1ValidatorBalanceTx(
		validationID,
		balance,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueDisableL1ValidatorTx(
	validationID ids.ID,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueDisableL1ValidatorTx(
		validationID,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueImportTx(
		sourceChainID,
		to,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*lux.TransferableOutput,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueExportTx(
		chainID,
		outputs,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTransformSubnetTx(
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
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueTransformSubnetTx(
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
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer vmsigner.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...pwallet.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueUnsignedTx(
		utx,
		pwallet.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTx(
	tx *txs.Tx,
	options ...pwallet.Option,
) error {
	return w.wallet.IssueTx(
		tx,
		pwallet.UnionOptions(w.options, options)...,
	)
}

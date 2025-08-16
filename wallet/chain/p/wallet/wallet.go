// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	vmsigner "github.com/luxfi/node/vms/platformvm/signer"
	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
)

var _ Wallet = (*wallet)(nil)

type Client interface {
	// IssueTx issues the signed tx.
	IssueTx(
		tx *txs.Tx,
		options ...common.Option,
	) error
}

type Wallet interface {
	Client

	// Builder returns the builder that will be used to create the transactions.
	Builder() builder.Builder

	// Signer returns the signer that will be used to sign the transactions.
	Signer() walletsigner.Signer

	// IssueBaseTx creates, signs, and issues a new simple value transfer.
	IssueBaseTx(
		outputs []*lux.TransferableOutput,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueAddValidatorTx creates, signs, and issues a new validator of the
	// primary network.
	IssueAddValidatorTx(
		vdr *txs.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueAddSubnetValidatorTx creates, signs, and issues a new validator of a
	// subnet.
	IssueAddSubnetValidatorTx(
		vdr *txs.SubnetValidator,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueRemoveSubnetValidatorTx creates, signs, and issues a transaction
	// that removes a validator of a subnet.
	IssueRemoveSubnetValidatorTx(
		nodeID ids.NodeID,
		subnetID ids.ID,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueAddDelegatorTx creates, signs, and issues a new delegator to a
	// validator on the primary network.
	IssueAddDelegatorTx(
		vdr *txs.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueCreateChainTx creates, signs, and issues a new chain in the named
	// subnet.
	IssueCreateChainTx(
		subnetID ids.ID,
		genesis []byte,
		vmID ids.ID,
		fxIDs []ids.ID,
		chainName string,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueCreateSubnetTx creates, signs, and issues a new subnet with the
	// specified owner.
	IssueCreateSubnetTx(
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueTransferSubnetOwnershipTx creates, signs, and issues a transaction that
	// changes the owner of the named subnet.
	IssueTransferSubnetOwnershipTx(
		subnetID ids.ID,
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueConvertSubnetToL1Tx creates, signs, and issues a transaction that
	// converts the subnet to a Permissionless L1.
	IssueConvertSubnetToL1Tx(
		subnetID ids.ID,
		chainID ids.ID,
		address []byte,
		validators []*txs.ConvertSubnetToL1Validator,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueRegisterL1ValidatorTx creates, signs, and issues a transaction that
	// adds a validator to an L1.
	IssueRegisterL1ValidatorTx(
		balance uint64,
		proofOfPossession [bls.SignatureLen]byte,
		message []byte,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueSetL1ValidatorWeightTx creates, signs, and issues a transaction that
	// sets the weight of a validator on an L1.
	IssueSetL1ValidatorWeightTx(
		message []byte,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueIncreaseL1ValidatorBalanceTx creates, signs, and issues a
	// transaction that increases the balance of a validator on an L1 for the
	// continuous fee.
	IssueIncreaseL1ValidatorBalanceTx(
		validationID ids.ID,
		balance uint64,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueDisableL1ValidatorTx creates, signs, and issues a transaction that
	// disables an L1 validator.
	IssueDisableL1ValidatorTx(
		validationID ids.ID,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueImportTx creates, signs, and issues an import transaction.
	IssueImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction.
	IssueExportTx(
		chainID ids.ID,
		outputs []*lux.TransferableOutput,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueTransformSubnetTx creates a transform subnet transaction.
	IssueTransformSubnetTx(
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
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueAddPermissionlessValidatorTx creates, signs, and issues a new
	// validator of the specified subnet.
	IssueAddPermissionlessValidatorTx(
		vdr *txs.SubnetValidator,
		signer vmsigner.Signer,
		assetID ids.ID,
		validationRewardsOwner *secp256k1fx.OutputOwners,
		delegationRewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueAddPermissionlessDelegatorTx creates, signs, and issues a new
	// delegator of the specified subnet.
	IssueAddPermissionlessDelegatorTx(
		vdr *txs.SubnetValidator,
		assetID ids.ID,
		rewardsOwner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueUnsignedTx signs and issues the unsigned tx.
	IssueUnsignedTx(
		utx txs.UnsignedTx,
		options ...common.Option,
	) (*txs.Tx, error)
}

func New(
	client Client,
	builder builder.Builder,
	signer walletsigner.Signer,
) Wallet {
	return &wallet{
		Client:  client,
		builder: builder,
		signer:  signer,
	}
}

type wallet struct {
	Client
	builder builder.Builder
	signer  walletsigner.Signer
}

func (w *wallet) Builder() builder.Builder {
	return w.builder
}

func (w *wallet) Signer() walletsigner.Signer {
	return w.signer
}

func (w *wallet) IssueBaseTx(
	outputs []*lux.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewBaseTx(outputs, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewAddValidatorTx(vdr, rewardsOwner, shares, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewAddSubnetValidatorTx(vdr, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewRemoveSubnetValidatorTx(nodeID, subnetID, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewAddDelegatorTx(vdr, rewardsOwner, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewCreateChainTx(subnetID, genesis, vmID, fxIDs, chainName, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewCreateSubnetTx(owner, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewTransferSubnetOwnershipTx(subnetID, owner, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueConvertSubnetToL1Tx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	validators []*txs.ConvertSubnetToL1Validator,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewConvertSubnetToL1Tx(subnetID, chainID, address, validators, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueRegisterL1ValidatorTx(
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
	message []byte,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewRegisterL1ValidatorTx(balance, proofOfPossession, message, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueSetL1ValidatorWeightTx(
	message []byte,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewSetL1ValidatorWeightTx(message, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueIncreaseL1ValidatorBalanceTx(
	validationID ids.ID,
	balance uint64,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewIncreaseL1ValidatorBalanceTx(validationID, balance, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueDisableL1ValidatorTx(
	validationID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewDisableL1ValidatorTx(validationID, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewImportTx(sourceChainID, to, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*lux.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewExportTx(chainID, outputs, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueTransformSubnetTx(
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
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewTransformSubnetTx(
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
		options...,
	)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer vmsigner.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		options...,
	)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		options...,
	)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...common.Option,
) (*txs.Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := walletsigner.SignUnsigned(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueTx(tx, options...)
}

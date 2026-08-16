// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package fee implements gas complexity calculations for platform transactions.
// LP-103 compliance is enforced by the dynamic fee calculator.
package fee

import (
	"errors"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/math"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// Wire byte-lengths of the primitive fields the intrinsic-bandwidth fee
// schedule prices. The schedule sums these per-field sizes to charge bandwidth
// gas; they are the fee model's own constants (no codec dependency). Values are
// the fixed on-wire sizes of each primitive, so the schedule stays stable and
// self-contained.
const (
	wireShortLen   = 2 // uint16
	wireIntLen     = 4 // uint32
	wireLongLen    = 8 // uint64
	wireVersionLen = 2 // fixed per-tx serialization-header budget
)

// Signature verification costs were conservatively based on benchmarks run on
// an AWS c5.xlarge instance.
const (
	intrinsicValidatorBandwidth = ids.NodeIDLen + // nodeID
		wireLongLen + // start
		wireLongLen + // end
		wireLongLen // weight

	intrinsicNetValidatorBandwidth = intrinsicValidatorBandwidth + // validator
		ids.IDLen // subchainID

	intrinsicOutputBandwidth = ids.IDLen + // assetID
		wireIntLen // output typeID

	intrinsicStakeableLockedOutputBandwidth = wireLongLen + // locktime
		wireIntLen // output typeID

	intrinsicSECP256k1FxOutputOwnersBandwidth = wireLongLen + // locktime
		wireIntLen + // threshold
		wireIntLen // num addresses

	intrinsicSECP256k1FxOutputBandwidth = wireLongLen + // amount
		intrinsicSECP256k1FxOutputOwnersBandwidth

	intrinsicInputBandwidth = ids.IDLen + // txID
		wireIntLen + // output index
		ids.IDLen + // assetID
		wireIntLen + // input typeID
		wireIntLen // credential typeID

	intrinsicStakeableLockedInputBandwidth = wireLongLen + // locktime
		wireIntLen // input typeID

	intrinsicSECP256k1FxInputBandwidth = wireIntLen + // num indices
		wireIntLen // num signatures

	intrinsicSECP256k1FxTransferableInputBandwidth = wireLongLen + // amount
		intrinsicSECP256k1FxInputBandwidth

	intrinsicSECP256k1FxSignatureBandwidth = wireIntLen + // signature index
		secp256k1.SignatureLen // signature length

	intrinsicSECP256k1FxSignatureCompute = 200 // secp256k1 signature verification time is around 200us

	intrinsicBLSAggregateCompute           = 5     // BLS public key aggregation time is around 5us
	intrinsicBLSVerifyCompute              = 1_000 // BLS verification time is around 1000us
	intrinsicBLSPublicKeyValidationCompute = 50    // BLS public key validation time is around 50us
	intrinsicBLSPoPVerifyCompute           = intrinsicBLSPublicKeyValidationCompute + intrinsicBLSVerifyCompute

	intrinsicWarpDBReads = 3 + 20 // chainID -> subchainID mapping + apply weight diffs + apply pk diffs + diff application reads

	intrinsicPoPBandwidth = bls.PublicKeyLen + // public key
		bls.SignatureLen // signature

	intrinsicInputDBRead = 1

	intrinsicInputDBWrite  = 1
	intrinsicOutputDBWrite = 1
)

var (
	_ txs.Visitor = (*complexityVisitor)(nil)

	IntrinsicAddChainValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicNetValidatorBandwidth + // netValidator
			wireIntLen + // netAuth typeID
			wireIntLen, // netAuthCredential typeID
		gas.DBRead:  3, // get net auth + check for net transformation + check for net conversion
		gas.DBWrite: 3, // put current staker + write weight diff + write pk diff
	}
	IntrinsicCreateChainTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // subchainID
			wireShortLen + // chainName length
			ids.IDLen + // vmID
			wireIntLen + // num fxIDs
			wireIntLen + // genesis length
			wireIntLen + // chainAuth typeID
			wireIntLen, // chainAuthCredential typeID
		gas.DBRead:  3, // get chain auth + check for chain transformation + check for chain conversion
		gas.DBWrite: 1, // put chain
	}
	IntrinsicCreateNetworkTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wireIntLen, // owner typeID
		gas.DBWrite: 1, // write chain owner
	}
	IntrinsicImportTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // source chainID
			wireIntLen, // num importing inputs
	}
	IntrinsicExportTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // destination chainID
			wireIntLen, // num exported outputs
	}
	IntrinsicRemoveChainValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.NodeIDLen + // nodeID
			ids.IDLen + // netID
			wireIntLen + // netAuth typeID
			wireIntLen, // netAuthCredential typeID
		gas.DBRead:  1, // read net auth
		gas.DBWrite: 3, // delete validator + write weight diff + write pk diff
	}
	IntrinsicAddPermissionlessValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subchainID
			wireIntLen + // signer typeID
			wireIntLen + // num stake outs
			wireIntLen + // validator rewards typeID
			wireIntLen + // delegator rewards typeID
			wireIntLen, // delegation shares
		gas.DBRead:  1, // get staking config
		gas.DBWrite: 3, // put current staker + write weight diff + write pk diff
	}
	IntrinsicAddPermissionlessDelegatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			intrinsicValidatorBandwidth + // validator
			ids.IDLen + // subchainID
			wireIntLen + // num stake outs
			wireIntLen, // delegator rewards typeID
		gas.DBRead:  1, // get staking config
		gas.DBWrite: 2, // put current staker + write weight diff
	}
	IntrinsicTransferChainOwnershipTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // netID
			wireIntLen + // netAuth typeID
			wireIntLen + // owner typeID
			wireIntLen, // netAuthCredential typeID
		gas.DBRead:  1, // read net auth
		gas.DBWrite: 1, // set net owner
	}
	IntrinsicBaseTxComplexities = gas.Dimensions{
		gas.Bandwidth: wireVersionLen + // codecVersion
			wireIntLen + // typeID
			wireIntLen + // networkID
			ids.IDLen + // blockchainID
			wireIntLen + // number of outputs
			wireIntLen + // number of inputs
			wireIntLen + // length of memo
			wireIntLen, // number of credentials
	}
	IntrinsicRegisterL1ValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wireLongLen + // balance
			bls.SignatureLen + // proof of possession
			wireIntLen, // message length
		gas.DBRead:  5, // conversion owner + expiry lookup + sov lookup + subchainID/nodeID lookup + weight lookup
		gas.DBWrite: 6, // write current staker + expiry + write weight diff + write pk diff + subchainID/nodeID lookup + weight lookup
		gas.Compute: intrinsicBLSPoPVerifyCompute,
	}
	IntrinsicSetL1ValidatorWeightTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			wireIntLen, // message length
		gas.DBRead:  3, // read staker + read conversion + read weight
		gas.DBWrite: 5, // remaining balance utxo + write weight diff + write pk diff + weights lookup + validator write
	}
	IntrinsicIncreaseL1ValidatorBalanceTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // validationID
			wireLongLen, // balance
		gas.DBRead:  1, // read staker
		gas.DBWrite: 5, // weight diff + deactivated weight diff + public key diff + delete staker + write staker
	}
	IntrinsicDisableL1ValidatorTxComplexities = gas.Dimensions{
		gas.Bandwidth: IntrinsicBaseTxComplexities[gas.Bandwidth] +
			ids.IDLen + // validationID
			wireIntLen + // auth typeID
			wireIntLen, // authCredential typeID
		gas.DBRead:  1, // read staker
		gas.DBWrite: 6, // write remaining balance utxo + weight diff + deactivated weight diff + public key diff + delete staker + write staker
	}

	errUnsupportedOutput = errors.New("unsupported output type")
	errUnsupportedInput  = errors.New("unsupported input type")
	errUnsupportedOwner  = errors.New("unsupported owner type")
	errUnsupportedAuth   = errors.New("unsupported auth type")
	errUnsupportedSigner = errors.New("unsupported signer type")
)

func TxComplexity(txs ...txs.UnsignedTx) (gas.Dimensions, error) {
	var (
		c          complexityVisitor
		complexity gas.Dimensions
	)
	for _, tx := range txs {
		c = complexityVisitor{}
		err := tx.Visit(&c)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&c.output)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

// OutputComplexity returns the complexity outputs add to a transaction.
func OutputComplexity(outs ...*lux.TransferableOutput) (gas.Dimensions, error) {
	var complexity gas.Dimensions
	for _, out := range outs {
		outputComplexity, err := outputComplexity(out)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&outputComplexity)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

func outputComplexity(out *lux.TransferableOutput) (gas.Dimensions, error) {
	complexity := gas.Dimensions{
		gas.Bandwidth: intrinsicOutputBandwidth + intrinsicSECP256k1FxOutputBandwidth,
		gas.DBWrite:   intrinsicOutputDBWrite,
	}

	outIntf := out.Out
	if stakeableOut, ok := outIntf.(*stakeable.LockOut); ok {
		complexity[gas.Bandwidth] += intrinsicStakeableLockedOutputBandwidth
		outIntf = stakeableOut.TransferableOut
	}

	secp256k1Out, ok := outIntf.(*secp256k1fx.TransferOutput)
	if !ok {
		return gas.Dimensions{}, errUnsupportedOutput
	}

	numAddresses := uint64(len(secp256k1Out.Addrs))
	addressBandwidth, err := math.Mul(numAddresses, ids.ShortIDLen)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(complexity[gas.Bandwidth], addressBandwidth)
	return complexity, err
}

// InputComplexity returns the complexity inputs add to a transaction.
// It includes the complexity that the corresponding credentials will add.
func InputComplexity(ins ...*lux.TransferableInput) (gas.Dimensions, error) {
	var complexity gas.Dimensions
	for _, in := range ins {
		inputComplexity, err := inputComplexity(in)
		if err != nil {
			return gas.Dimensions{}, err
		}

		complexity, err = complexity.Add(&inputComplexity)
		if err != nil {
			return gas.Dimensions{}, err
		}
	}
	return complexity, nil
}

func inputComplexity(in *lux.TransferableInput) (gas.Dimensions, error) {
	complexity := gas.Dimensions{
		gas.Bandwidth: intrinsicInputBandwidth + intrinsicSECP256k1FxTransferableInputBandwidth,
		gas.DBRead:    intrinsicInputDBRead,
		gas.DBWrite:   intrinsicInputDBWrite,
	}

	inIntf := in.In
	if stakeableIn, ok := inIntf.(*stakeable.LockIn); ok {
		complexity[gas.Bandwidth] += intrinsicStakeableLockedInputBandwidth
		inIntf = stakeableIn.TransferableIn
	}

	secp256k1In, ok := inIntf.(*secp256k1fx.TransferInput)
	if !ok {
		return gas.Dimensions{}, errUnsupportedInput
	}

	numSignatures := uint64(len(secp256k1In.SigIndices))
	// Add signature bandwidth
	signatureBandwidth, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(complexity[gas.Bandwidth], signatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	// Add signature compute
	complexity[gas.Compute], err = math.Mul(numSignatures, intrinsicSECP256k1FxSignatureCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}
	return complexity, err
}

// OwnerComplexity returns the complexity an owner adds to a transaction.
// It does not include the typeID of the owner.
func OwnerComplexity(ownerIntf fx.Owner) (gas.Dimensions, error) {
	owner, ok := ownerIntf.(*secp256k1fx.OutputOwners)
	if !ok {
		return gas.Dimensions{}, errUnsupportedOwner
	}

	numAddresses := uint64(len(owner.Addrs))
	addressBandwidth, err := math.Mul(numAddresses, ids.ShortIDLen)
	if err != nil {
		return gas.Dimensions{}, err
	}

	bandwidth, err := math.Add(addressBandwidth, intrinsicSECP256k1FxOutputOwnersBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: bandwidth,
	}, nil
}

// AuthComplexity returns the complexity an authorization adds to a transaction.
// It does not include the typeID of the authorization.
// It does includes the complexity that the corresponding credential will add.
// It does not include the typeID of the credential.
func AuthComplexity(authIntf verify.Verifiable) (gas.Dimensions, error) {
	auth, ok := authIntf.(*secp256k1fx.Input)
	if !ok {
		return gas.Dimensions{}, errUnsupportedAuth
	}

	numSignatures := uint64(len(auth.SigIndices))
	signatureBandwidth, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	bandwidth, err := math.Add(signatureBandwidth, intrinsicSECP256k1FxInputBandwidth)
	if err != nil {
		return gas.Dimensions{}, err
	}

	signatureCompute, err := math.Mul(numSignatures, intrinsicSECP256k1FxSignatureCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: bandwidth,
		gas.Compute:   signatureCompute,
	}, nil
}

// SignerComplexity returns the complexity a signer adds to a transaction.
// It does not include the typeID of the signer.
func SignerComplexity(s signer.Signer) (gas.Dimensions, error) {
	switch s.(type) {
	case *signer.Empty:
		return gas.Dimensions{}, nil
	case *signer.ProofOfPossession:
		return gas.Dimensions{
			gas.Bandwidth: intrinsicPoPBandwidth,
			gas.Compute:   intrinsicBLSPoPVerifyCompute,
		}, nil
	default:
		return gas.Dimensions{}, errUnsupportedSigner
	}
}

// WarpComplexity returns the complexity a warp message adds to a transaction.
func WarpComplexity(message []byte) (gas.Dimensions, error) {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return gas.Dimensions{}, err
	}

	numSigners, err := msg.Signature.NumSigners()
	if err != nil {
		return gas.Dimensions{}, err
	}
	aggregationCompute, err := math.Mul(uint64(numSigners), intrinsicBLSAggregateCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	compute, err := math.Add(aggregationCompute, intrinsicBLSVerifyCompute)
	if err != nil {
		return gas.Dimensions{}, err
	}

	return gas.Dimensions{
		gas.Bandwidth: uint64(len(message)),
		gas.DBRead:    intrinsicWarpDBReads,
		gas.Compute:   compute,
	}, nil
}

type complexityVisitor struct {
	output gas.Dimensions
}

func (*complexityVisitor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrUnsupportedTx
}

func (*complexityVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTx
}

func (c *complexityVisitor) CreateChainTx(tx *txs.CreateChainTx) error {
	bandwidth, err := math.Mul(uint64(len(tx.FxIDs())), ids.IDLen)
	if err != nil {
		return err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(tx.BlockchainName())))
	if err != nil {
		return err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(tx.GenesisData())))
	if err != nil {
		return err
	}
	dynamicComplexity := gas.Dimensions{
		gas.Bandwidth: bandwidth,
	}

	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.ChainAuth())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicCreateChainTxComplexities.Add(
		&dynamicComplexity,
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) ImportTx(tx *txs.ImportTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	inputsComplexity, err := InputComplexity(tx.ImportedInputs()...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicImportTxComplexities.Add(
		&baseTxComplexity,
		&inputsComplexity,
	)
	return err
}

func (c *complexityVisitor) ExportTx(tx *txs.ExportTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.ExportedOutputs()...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicExportTxComplexities.Add(
		&baseTxComplexity,
		&outputsComplexity,
	)
	return err
}

func (c *complexityVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	signerComplexity, err := SignerComplexity(tx.Signer())
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.StakeOuts()...)
	if err != nil {
		return err
	}
	validatorOwnerComplexity, err := OwnerComplexity(tx.ValidatorRewardsOwner())
	if err != nil {
		return err
	}
	delegatorOwnerComplexity, err := OwnerComplexity(tx.DelegatorRewardsOwner())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddPermissionlessValidatorTxComplexities.Add(
		&baseTxComplexity,
		&signerComplexity,
		&outputsComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
	return err
}

func (c *complexityVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.DelegationRewardsOwner())
	if err != nil {
		return err
	}
	outputsComplexity, err := OutputComplexity(tx.StakeOuts()...)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddPermissionlessDelegatorTxComplexities.Add(
		&baseTxComplexity,
		&ownerComplexity,
		&outputsComplexity,
	)
	return err
}

func (c *complexityVisitor) BaseTx(tx *txs.BaseTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicBaseTxComplexities.Add(&baseTxComplexity)
	return err
}

func (c *complexityVisitor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	warpComplexity, err := WarpComplexity(tx.Message())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicRegisterL1ValidatorTxComplexities.Add(
		&baseTxComplexity,
		&warpComplexity,
	)
	return err
}

func (c *complexityVisitor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	warpComplexity, err := WarpComplexity(tx.Message())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicSetL1ValidatorWeightTxComplexities.Add(
		&baseTxComplexity,
		&warpComplexity,
	)
	return err
}

func (c *complexityVisitor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	c.output, err = IntrinsicIncreaseL1ValidatorBalanceTxComplexities.Add(
		&baseTxComplexity,
	)
	return err
}

func (c *complexityVisitor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.DisableAuth())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicDisableL1ValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

// baseTxEnvelope is the spending-envelope accessor surface shared by every
// spending tx type (BaseTx and all delta-carrying txs embed spendingTx, which
// serves Outputs/Inputs/Memo). baseTxComplexity reads it directly rather than
// reaching for a struct field that no longer exists.
type baseTxEnvelope interface {
	Outputs() []*lux.TransferableOutput
	Inputs() []*lux.TransferableInput
	Memo() []byte
}

func baseTxComplexity(tx baseTxEnvelope) (gas.Dimensions, error) {
	outputsComplexity, err := OutputComplexity(tx.Outputs()...)
	if err != nil {
		return gas.Dimensions{}, err
	}
	inputsComplexity, err := InputComplexity(tx.Inputs()...)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity, err := outputsComplexity.Add(&inputsComplexity)
	if err != nil {
		return gas.Dimensions{}, err
	}
	complexity[gas.Bandwidth], err = math.Add(
		complexity[gas.Bandwidth],
		uint64(len(tx.Memo())),
	)
	return complexity, err
}

func (c *complexityVisitor) AddChainValidatorTx(tx *txs.AddChainValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.ChainAuth())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicAddChainValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (c *complexityVisitor) CreateNetworkTx(tx *txs.CreateNetworkTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicCreateNetworkTxComplexities.Add(
		&baseTxComplexity,
		&ownerComplexity,
	)
	return err
}

// ConvertNetworkTx promotes an existing network to a sovereign L1: it carries
// the manager address plus the initial validator set. Complexity is the base
// spend + auth, plus the raw wire bandwidth of the manager address and of each
// validator record, and one staker DBWrite per validator. No invented
// intrinsic dimension table — bandwidth is the byte count of the variable
// payload, its definitional lower bound.
func (c *complexityVisitor) ConvertNetworkTx(tx *txs.ConvertNetworkTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.Auth())
	if err != nil {
		return err
	}
	c.output, err = baseTxComplexity.Add(&authComplexity)
	if err != nil {
		return err
	}

	// Manager address bandwidth.
	c.output[gas.Bandwidth], err = math.Add(c.output[gas.Bandwidth], uint64(len(tx.ManagerAddress())))
	if err != nil {
		return err
	}

	// Per-validator: NodeID + weight(8) + balance(8) + PoP signer(pub+sig) +
	// both owners' addresses, plus one staker write each.
	for _, vdr := range tx.Validators() {
		vdrBandwidth := uint64(len(vdr.NodeID)) + 2*8 + // weight(8) + balance(8)
			uint64(len(vdr.Signer.PublicKey)) + uint64(len(vdr.Signer.ProofOfPossession)) +
			uint64(len(vdr.RemainingBalanceOwner.Addresses)+len(vdr.DeactivationOwner.Addresses))*ids.ShortIDLen
		c.output[gas.Bandwidth], err = math.Add(c.output[gas.Bandwidth], vdrBandwidth)
		if err != nil {
			return err
		}
		c.output[gas.DBWrite], err = math.Add(c.output[gas.DBWrite], 1)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *complexityVisitor) RemoveChainValidatorTx(tx *txs.RemoveChainValidatorTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.ChainAuth())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicRemoveChainValidatorTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
	)
	return err
}

func (*complexityVisitor) TransformChainTx(*txs.TransformChainTx) error {
	return ErrUnsupportedTx
}

func (c *complexityVisitor) TransferChainOwnershipTx(tx *txs.TransferChainOwnershipTx) error {
	baseTxComplexity, err := baseTxComplexity(tx)
	if err != nil {
		return err
	}
	authComplexity, err := AuthComplexity(tx.ChainAuth())
	if err != nil {
		return err
	}
	ownerComplexity, err := OwnerComplexity(tx.Owner())
	if err != nil {
		return err
	}
	c.output, err = IntrinsicTransferChainOwnershipTxComplexities.Add(
		&baseTxComplexity,
		&authComplexity,
		&ownerComplexity,
	)
	return err
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// The fee model has exactly two answers for a transaction: a price, or a
// refusal. The tests below pin the boundary from the outside — a priced tx is
// never free, a refused tx never carries a number, and every variable-length
// field a submitter controls raises the price strictly. Each one fails if the
// arm it covers is deleted or short-circuited.

func feeOwner() *secp256k1fx.OutputOwners {
	return &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
}

func feeAuth() *secp256k1fx.Input {
	return &secp256k1fx.Input{SigIndices: []uint32{0}}
}

func feeValidator() txs.Validator {
	return txs.Validator{
		NodeID: ids.GenerateTestNodeID(),
		Start:  1,
		End:    2,
		Wght:   3,
	}
}

func feeStakeOuts() []*lux.TransferableOutput {
	return []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: ids.GenerateTestID()},
		Out: &secp256k1fx.TransferOutput{
			Amt:          1000,
			OutputOwners: *feeOwner(),
		},
	}}
}

// feeWarp builds a parseable warp message with the requested signer count and
// payload size, so bandwidth and compute can be varied independently.
func feeWarp(t *testing.T, numSigners int, payloadLen int) []byte {
	t.Helper()
	unsigned, err := warp.NewUnsignedMessage(10, ids.GenerateTestID(), make([]byte, payloadLen))
	require.NoError(t, err)

	signers := set.NewBits()
	for i := 0; i < numSigners; i++ {
		signers.Add(i)
	}
	msg, err := warp.NewMessage(unsigned, &warp.BitSetSignature{Signers: signers.Bytes()})
	require.NoError(t, err)
	return msg.Bytes()
}

// pricedTxs returns one transaction of every type the fee model prices. A type
// missing here is a type nothing below constrains.
func pricedTxs(t *testing.T) map[string]txs.UnsignedTx {
	t.Helper()

	build := func(tx txs.UnsignedTx, err error) txs.UnsignedTx {
		t.Helper()
		require.NoError(t, err)
		return tx
	}

	warpMsg := feeWarp(t, 4, 32)

	return map[string]txs.UnsignedTx{
		"BaseTx":     build(txs.NewBaseTx(feeSpendBase())),
		"CreateChain": build(txs.NewCreateChainTx(
			feeSpendBase(), ids.GenerateTestID(), "chain", ids.GenerateTestID(),
			nil, []byte("genesis"), feeAuth(),
		)),
		"CreateNetwork": build(txs.NewCreateNetworkTx(
			feeSpendBase(), ids.GenerateTestID(), feeOwner(),
			security.Mode{RestakeParent: true}, nil, ids.GenerateTestID(), []byte("manager"),
		)),
		"Import": build(txs.NewImportTx(
			feeSpendBase(), ids.GenerateTestID(), feeSpendBase().Ins,
		)),
		"Export": build(txs.NewExportTx(
			feeSpendBase(), ids.GenerateTestID(), feeSpendBase().Outs,
		)),
		"AddChainValidator": build(txs.NewAddChainValidatorTx(
			feeSpendBase(), feeValidator(), ids.GenerateTestID(), feeAuth(),
		)),
		"RemoveChainValidator": build(txs.NewRemoveChainValidatorTx(
			feeSpendBase(), ids.GenerateTestNodeID(), ids.GenerateTestID(), feeAuth(),
		)),
		"TransferChainOwnership": build(txs.NewTransferChainOwnershipTx(
			feeSpendBase(), ids.GenerateTestID(), feeAuth(), feeOwner(),
		)),
		"AddPermissionlessValidator": build(txs.NewAddPermissionlessValidatorTx(
			feeSpendBase(), feeValidator(), ids.GenerateTestID(), &signer.Empty{},
			feeStakeOuts(), feeOwner(), feeOwner(), 1,
		)),
		"AddPermissionlessDelegator": build(txs.NewAddPermissionlessDelegatorTx(
			feeSpendBase(), feeValidator(), ids.GenerateTestID(), feeStakeOuts(), feeOwner(),
		)),
		"RegisterL1Validator": build(txs.NewRegisterL1ValidatorTx(
			feeSpendBase(), 1000, [bls.SignatureLen]byte{}, warpMsg,
		)),
		"SetL1ValidatorWeight": build(txs.NewSetL1ValidatorWeightTx(feeSpendBase(), warpMsg)),
		"IncreaseL1ValidatorBalance": build(txs.NewIncreaseL1ValidatorBalanceTx(
			feeSpendBase(), ids.GenerateTestID(), 1000,
		)),
		"DisableL1Validator": build(txs.NewDisableL1ValidatorTx(
			feeSpendBase(), ids.GenerateTestID(), feeAuth(),
		)),
		"ConvertNetwork": build(feeConvertNetworkTx(t, 1)),
	}
}

func feeConvertNetworkTx(t *testing.T, numValidators int) (*txs.ConvertNetworkTx, error) {
	t.Helper()

	validators := make([]*txs.NetworkValidator, numValidators)
	for i := range validators {
		validators[i] = &txs.NetworkValidator{
			NodeID:  ids.GenerateTestNodeID().Bytes(),
			Weight:  1,
			Balance: 1,
			Signer:  signer.ProofOfPossession{},
		}
	}
	return txs.NewConvertNetworkTx(
		feeSpendBase(),
		ids.GenerateTestID(), // network
		ids.GenerateTestID(), // parent
		ids.GenerateTestID(), // managerChainID
		security.Mode{RestakeParent: true},
		[]byte("manager"),
		validators,
		feeAuth(),
	)
}

// A transaction the fee model accepts must cost something. An arm that returns
// nil without recording complexity would sell blockspace for zero.
func TestPricedTxIsNeverFree(t *testing.T) {
	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)

	for name, utx := range pricedTxs(t) {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			complexity, err := TxComplexity(utx)
			require.NoError(err)
			require.NotZero(complexity[gas.Bandwidth])

			fee, err := calculator.CalculateFee(utx)
			require.NoError(err)
			require.NotZero(fee)
		})
	}
}

// A transaction the fee model cannot price must be refused, and the refusal
// must carry no number. Answering (0, nil) here is how an unpriceable type
// becomes free blockspace instead of a rejected transaction.
func TestRefusedTxCarriesNoFee(t *testing.T) {
	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)

	addValidator, err := txs.NewAddValidatorTx(
		feeSpendBase(), feeValidator(), feeStakeOuts(), feeOwner(), 1,
	)
	require.NoError(t, err)

	addDelegator, err := txs.NewAddDelegatorTx(
		feeSpendBase(), feeValidator(), feeStakeOuts(), feeOwner(),
	)
	require.NoError(t, err)

	transformChain, err := txs.NewTransformChainTx(
		feeSpendBase(), ids.GenerateTestID(), ids.GenerateTestID(),
		1, 2, 1, 2, 1, 2, 1, 2, 1, 1, 1, 1, feeAuth(),
	)
	require.NoError(t, err)

	refused := map[string]txs.UnsignedTx{
		"AddValidator":    addValidator,
		"AddDelegator":    addDelegator,
		"RewardValidator": txs.NewRewardValidatorTx(ids.GenerateTestID()),
		"TransformChain":  transformChain,
	}

	for name, utx := range refused {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			_, err := TxComplexity(utx)
			require.ErrorIs(err, ErrUnsupportedTx)

			fee, err := calculator.CalculateFee(utx)
			require.ErrorIs(err, ErrUnsupportedTx)
			require.Zero(fee)
		})
	}
}

// Warp bandwidth must track the message the submitter chose, and a message
// that does not parse must be refused rather than priced at zero. Either gap
// lets an arbitrarily large warp payload ride for a fixed price.
func TestWarpPayloadIsPriced(t *testing.T) {
	require := require.New(t)

	small, err := WarpComplexity(feeWarp(t, 1, 32))
	require.NoError(err)

	large, err := WarpComplexity(feeWarp(t, 1, 4096))
	require.NoError(err)
	require.Greater(large[gas.Bandwidth], small[gas.Bandwidth])

	// Aggregation cost is per signer, so more signers must verify dearer.
	manySigners, err := WarpComplexity(feeWarp(t, 64, 32))
	require.NoError(err)
	require.Greater(manySigners[gas.Compute], small[gas.Compute])

	// A message that does not parse yields an error and no dimensions.
	complexity, err := WarpComplexity([]byte("not a warp message"))
	require.Error(err)
	require.Zero(complexity)
}

// Convert cost must scale with the validator set the transaction carries.
// A flat price would let one transaction install an unbounded validator set.
func TestConvertCostScalesWithValidators(t *testing.T) {
	require := require.New(t)

	one, err := feeConvertNetworkTx(t, 1)
	require.NoError(err)
	two, err := feeConvertNetworkTx(t, 2)
	require.NoError(err)

	oneComplexity, err := TxComplexity(one)
	require.NoError(err)
	twoComplexity, err := TxComplexity(two)
	require.NoError(err)

	require.Greater(twoComplexity[gas.Bandwidth], oneComplexity[gas.Bandwidth])
	require.Greater(twoComplexity[gas.DBWrite], oneComplexity[gas.DBWrite])
}

// Every variable-length part of the spending envelope must raise the price.
// Any one of these left uncharged is free space inside an otherwise paid tx.
func TestSpendingEnvelopeIsFullyCharged(t *testing.T) {
	require := require.New(t)

	baseline, err := TxComplexity(feeBaseTx(t))
	require.NoError(err)

	withMemo := feeSpendBase()
	withMemo.Memo = []byte("a memo the submitter chose")
	memoTx, err := txs.NewBaseTx(withMemo)
	require.NoError(err)
	memoComplexity, err := TxComplexity(memoTx)
	require.NoError(err)
	require.Greater(memoComplexity[gas.Bandwidth], baseline[gas.Bandwidth])

	withOutput := feeSpendBase()
	withOutput.Outs = append(withOutput.Outs, feeStakeOuts()...)
	outputTx, err := txs.NewBaseTx(withOutput)
	require.NoError(err)
	outputComplexity, err := TxComplexity(outputTx)
	require.NoError(err)
	require.Greater(outputComplexity[gas.Bandwidth], baseline[gas.Bandwidth])
	require.Greater(outputComplexity[gas.DBWrite], baseline[gas.DBWrite])

	withInput := feeSpendBase()
	withInput.Ins = append(withInput.Ins, feeSpendBase().Ins...)
	inputTx, err := txs.NewBaseTx(withInput)
	require.NoError(err)
	inputComplexity, err := TxComplexity(inputTx)
	require.NoError(err)
	require.Greater(inputComplexity[gas.Bandwidth], baseline[gas.Bandwidth])
	require.Greater(inputComplexity[gas.DBRead], baseline[gas.DBRead])
}

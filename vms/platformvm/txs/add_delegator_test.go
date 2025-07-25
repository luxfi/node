// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus/snowtest"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var preFundedKeys = secp256k1.TestKeys()

func TestAddDelegatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	ctx := snowtest.Context(t, snowtest.PChainID)
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

	var (
		stx            *Tx
		addDelegatorTx *AddDelegatorTx
		err            error
	)

	// Case : signed tx is nil
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, ErrNilSignedTx)

	// Case : unsigned tx is nil
	err = addDelegatorTx.SyntacticVerify(ctx)
	require.ErrorIs(err, ErrNilTx)

	validatorWeight := uint64(2022)
	inputs := []*lux.TransferableInput{{
		UTXOID: lux.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: lux.Asset{ID: ctx.LUXAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: ctx.LUXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
			},
		},
	}}
	stakes := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: ctx.LUXAssetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(clk.Time().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: validatorWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}}
	addDelegatorTx = &AddDelegatorTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Outs:         outputs,
			Ins:          inputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Validator: Validator{
			NodeID: ctx.NodeID,
			Start:  uint64(clk.Time().Unix()),
			End:    uint64(clk.Time().Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
	}

	// Case: signed tx not initialized
	stx = &Tx{Unsigned: addDelegatorTx}
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, errSignedTxNotInitialized)

	// Case: valid tx
	stx, err = NewSigned(addDelegatorTx, Codec, signers)
	require.NoError(err)
	require.NoError(stx.SyntacticVerify(ctx))

	// Case: Wrong network ID
	addDelegatorTx.SyntacticallyVerified = false
	addDelegatorTx.NetworkID++
	stx, err = NewSigned(addDelegatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, lux.ErrWrongNetworkID)
	addDelegatorTx.NetworkID--

	// Case: delegator weight is not equal to total stake weight
	addDelegatorTx.SyntacticallyVerified = false
	addDelegatorTx.Wght = 2 * validatorWeight
	stx, err = NewSigned(addDelegatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, errDelegatorWeightMismatch)
	addDelegatorTx.Wght = validatorWeight
}

func TestAddDelegatorTxSyntacticVerifyNotLUX(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	ctx := snowtest.Context(t, snowtest.PChainID)
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

	var (
		stx            *Tx
		addDelegatorTx *AddDelegatorTx
		err            error
	)

	assetID := ids.GenerateTestID()
	validatorWeight := uint64(2022)
	inputs := []*lux.TransferableInput{{
		UTXOID: lux.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: lux.Asset{ID: assetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
			},
		},
	}}
	stakes := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(clk.Time().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: validatorWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}}
	addDelegatorTx = &AddDelegatorTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
			Outs:         outputs,
			Ins:          inputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Validator: Validator{
			NodeID: ctx.NodeID,
			Start:  uint64(clk.Time().Unix()),
			End:    uint64(clk.Time().Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
	}

	stx, err = NewSigned(addDelegatorTx, Codec, signers)
	require.NoError(err)

	err = stx.SyntacticVerify(ctx)
	require.ErrorIs(err, errStakeMustBeLUX)
}

func TestAddDelegatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddDelegatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}

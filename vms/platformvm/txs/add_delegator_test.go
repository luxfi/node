// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// preFundedKeys is the shared set of test signing keys used across the txs
// package tests (owner addresses, credentials).
var preFundedKeys = secp256k1.TestKeys()

func TestAddDelegatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	owner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{preFundedKeys[0].Address()}}
	weight := uint64(2022)
	validator := Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: weight}
	stakeOuts := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: rt.UTXOAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: weight, OutputOwners: *owner},
	}}
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: signed tx is nil
	var stx *Tx
	require.ErrorIs(stx.SyntacticVerify(rt), ErrNilSignedTx)

	// Case: unsigned tx is nil
	var nilTx *AddDelegatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: signed tx not initialized (Tx wrapper without Initialize/Sign)
	u, err := NewAddDelegatorTx(base(rt.NetworkID), validator, stakeOuts, owner)
	require.NoError(err)
	require.ErrorIs((&Tx{Unsigned: u}).SyntacticVerify(rt), errSignedTxNotInitialized)

	// Case: valid tx
	require.NoError(u.SyntacticVerify(rt))

	// Case: wrong network ID
	uWrong, err := NewAddDelegatorTx(base(rt.NetworkID+1), validator, stakeOuts, owner)
	require.NoError(err)
	require.ErrorIs(uWrong.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: delegator weight is not equal to total stake weight
	heavyValidator := validator
	heavyValidator.Wght = 2 * weight
	uMismatch, err := NewAddDelegatorTx(base(rt.NetworkID), heavyValidator, stakeOuts, owner)
	require.NoError(err)
	require.ErrorIs(uMismatch.SyntacticVerify(rt), errDelegatorWeightMismatch)
}

func TestAddDelegatorTxSyntacticVerifyNotLUX(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	owner := &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{preFundedKeys[0].Address()}}
	weight := uint64(2022)
	validator := Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: weight}
	// Stake asset is not the primary-network UTXO asset.
	stakeOuts := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: ids.GenerateTestID()},
		Out:   &secp256k1fx.TransferOutput{Amt: weight, OutputOwners: *owner},
	}}

	u, err := NewAddDelegatorTx(&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID}, validator, stakeOuts, owner)
	require.NoError(err)
	require.ErrorIs(u.SyntacticVerify(rt), errStakeMustBeLUX)
}

func TestAddDelegatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddDelegatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}

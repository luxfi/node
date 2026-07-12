// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestAddChainValidatorTxSyntacticVerify pins the (deprecated) per-chain
// validator-registration invariants. Every (in)valid case is expressed by
// passing values THROUGH the NewAddChainValidatorTx constructor.
func TestAddChainValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	rt := consensustest.Runtime(t, ids.GenerateTestID())

	weight := uint64(2022)
	chain := ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'I', 'D'}
	goodAuth := func() *secp256k1fx.Input { return &secp256k1fx.Input{SigIndices: []uint32{0, 1}} }
	validator := func(wght uint64) Validator {
		return Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: wght}
	}
	base := func(networkID uint32) *lux.BaseTx {
		return &lux.BaseTx{NetworkID: networkID, BlockchainID: rt.ChainID}
	}

	// Case: signed tx is nil
	var stx *Tx
	require.ErrorIs(stx.SyntacticVerify(rt), ErrNilSignedTx)

	// Case: unsigned tx is nil
	var nilTx *AddChainValidatorTx
	require.ErrorIs(nilTx.SyntacticVerify(rt), ErrNilTx)

	// Case: valid tx
	valid, err := NewAddChainValidatorTx(base(rt.NetworkID), validator(weight), chain, goodAuth())
	require.NoError(err)
	require.NoError(valid.SyntacticVerify(rt))

	// Case: wrong network ID
	wrongNet, err := NewAddChainValidatorTx(base(rt.NetworkID+1), validator(weight), chain, goodAuth())
	require.NoError(err)
	require.ErrorIs(wrongNet.SyntacticVerify(rt), lux.ErrWrongNetworkID)

	// Case: specifies primary network ChainID (empty)
	primaryEmpty, err := NewAddChainValidatorTx(base(rt.NetworkID), validator(weight), ids.Empty, goodAuth())
	require.NoError(err)
	require.ErrorIs(primaryEmpty.SyntacticVerify(rt), errAddPrimaryNetworkValidator)

	// Case: no weight
	noWeight, err := NewAddChainValidatorTx(base(rt.NetworkID), validator(0), chain, goodAuth())
	require.NoError(err)
	require.ErrorIs(noWeight.SyntacticVerify(rt), ErrWeightTooSmall)

	// Case: chain auth indices not sorted+unique ([1,1])
	badAuth, err := NewAddChainValidatorTx(base(rt.NetworkID), validator(weight), chain, &secp256k1fx.Input{SigIndices: []uint32{1, 1}})
	require.NoError(err)
	require.ErrorIs(badAuth.SyntacticVerify(rt), secp256k1fx.ErrInputIndicesNotSortedUnique)

	// Case: adding to Primary Network (explicit PrimaryNetworkID)
	primary, err := NewAddChainValidatorTx(base(rt.NetworkID), validator(weight), constants.PrimaryNetworkID, goodAuth())
	require.NoError(err)
	require.ErrorIs(primary.SyntacticVerify(rt), errAddPrimaryNetworkValidator)
}

// TestAddChainValidatorTx_RoundTrip replaces the deleted linearcodec
// Marshal/Parse golden test: the delta fields (Validator, Chain, ChainAuth)
// survive the struct-is-wire serialize→Parse path.
func TestAddChainValidatorTx_RoundTrip(t *testing.T) {
	require := require.New(t)

	vdr := Validator{NodeID: ids.GenerateTestNodeID(), Start: 0, End: 3600, Wght: 2022}
	chain := ids.GenerateTestID()
	auth := &secp256k1fx.Input{SigIndices: []uint32{0, 1}}

	utx, err := NewAddChainValidatorTx(spendBase(), vdr, chain, auth)
	require.NoError(err)

	got := roundTrip(t, utx).(*AddChainValidatorTx)
	require.Equal(vdr, got.Validator())
	require.Equal(chain, got.Chain())
	require.Equal(utx.ChainAuth(), got.ChainAuth())
}

func TestAddChainValidatorTxNotValidatorTx(t *testing.T) {
	txIntf := any((*AddChainValidatorTx)(nil))
	_, ok := txIntf.(ValidatorTx)
	require.False(t, ok)
}

func TestAddChainValidatorTxNotDelegatorTx(t *testing.T) {
	txIntf := any((*AddChainValidatorTx)(nil))
	_, ok := txIntf.(DelegatorTx)
	require.False(t, ok)
}

func TestAddChainValidatorTxNotPermissionlessStaker(t *testing.T) {
	txIntf := any((*AddChainValidatorTx)(nil))
	_, ok := txIntf.(PermissionlessStaker)
	require.False(t, ok)
}

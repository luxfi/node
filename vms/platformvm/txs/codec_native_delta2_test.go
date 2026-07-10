// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func sampleInput() *lux.TransferableInput {
	return &lux.TransferableInput{
		UTXOID: lux.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 2},
		Asset:  lux.Asset{ID: ids.GenerateTestID()},
		In:     &secp256k1fx.TransferInput{Amt: 500, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
	}
}

func sampleOutput() *lux.TransferableOutput {
	return &lux.TransferableOutput{
		Asset: lux.Asset{ID: ids.GenerateTestID()},
		Out: &secp256k1fx.TransferOutput{
			Amt:          600,
			OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		},
	}
}

func TestNative_TransferChainOwnership_RoundTrip(t *testing.T) {
	require := require.New(t)
	var owner fx.Owner = &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	in := &TransferChainOwnershipTx{
		BaseTx:    deltaBase(),
		Chain:     ids.GenerateTestID(),
		ChainAuth: &secp256k1fx.Input{SigIndices: []uint32{0, 1}},
		Owner:     owner,
	}
	got := roundTripUnsigned(t, in).(*TransferChainOwnershipTx)
	require.Equal(in.Chain, got.Chain)
	require.Equal(in.ChainAuth, got.ChainAuth)
	require.Equal(in.Owner, got.Owner)
}

func TestNative_RegisterL1Validator_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &RegisterL1ValidatorTx{BaseTx: deltaBase(), Balance: 9_000, Message: []byte("register-msg")}
	for i := range in.ProofOfPossession {
		in.ProofOfPossession[i] = byte(i)
	}
	got := roundTripUnsigned(t, in).(*RegisterL1ValidatorTx)
	require.Equal(in.Balance, got.Balance)
	require.Equal(in.ProofOfPossession, got.ProofOfPossession)
	require.Equal([]byte(in.Message), []byte(got.Message))
}

func TestNative_Import_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &ImportTx{
		BaseTx:         deltaBase(),
		SourceChain:    ids.GenerateTestID(),
		ImportedInputs: []*lux.TransferableInput{sampleInput(), sampleInput()},
	}
	got := roundTripUnsigned(t, in).(*ImportTx)
	require.Equal(in.SourceChain, got.SourceChain)
	require.Equal(in.ImportedInputs, got.ImportedInputs)
}

func TestNative_Export_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &ExportTx{
		BaseTx:           deltaBase(),
		DestinationChain: ids.GenerateTestID(),
		ExportedOutputs:  []*lux.TransferableOutput{sampleOutput()},
	}
	got := roundTripUnsigned(t, in).(*ExportTx)
	require.Equal(in.DestinationChain, got.DestinationChain)
	require.Equal(in.ExportedOutputs, got.ExportedOutputs)
}

func TestNative_CreateChain_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &CreateChainTx{
		BaseTx:         deltaBase(),
		ChainID:        ids.GenerateTestID(),
		VMID:           ids.GenerateTestID(),
		BlockchainName: "my-l1-chain",
		FxIDs:          []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()},
		GenesisData:    []byte(`{"genesis":true}`),
		ChainAuth:      &secp256k1fx.Input{SigIndices: []uint32{0}},
	}
	got := roundTripUnsigned(t, in).(*CreateChainTx)
	require.Equal(in.ChainID, got.ChainID)
	require.Equal(in.VMID, got.VMID)
	require.Equal(in.BlockchainName, got.BlockchainName)
	require.Equal(in.FxIDs, got.FxIDs)
	require.Equal([]byte(in.GenesisData), []byte(got.GenesisData))
	require.Equal(in.ChainAuth, got.ChainAuth)
}

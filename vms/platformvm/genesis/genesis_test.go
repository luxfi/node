// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
)

func createTestGenesis(t *testing.T) *Genesis {
	// Create a basic genesis with minimal data
	utxo := &UTXO{
		UTXO: lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: 0,
			},
			Asset: lux.Asset{
				ID: ids.GenerateTestID(),
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: 123456789,
			},
		},
		Message: []byte("test message"),
	}

	genesis := &Genesis{
		UTXOs:         []*UTXO{utxo},
		Validators:    []*txs.Tx{},
		Chains:        []*txs.Tx{},
		Timestamp:     5,
		InitialSupply: 123456789,
		Message:       "Test Genesis",
	}

	return genesis
}

func TestParseGenesis(t *testing.T) {
	// Test that parsing genesis works correctly
	genesis := createTestGenesis(t)
	
	// Encode the genesis
	bytes, err := Codec.Marshal(CodecVersion, genesis)
	require.NoError(t, err)
	
	// Parse it back
	parsed, err := Parse(bytes)
	require.NoError(t, err)
	require.Equal(t, genesis.Timestamp, parsed.Timestamp)
	require.Equal(t, genesis.InitialSupply, parsed.InitialSupply)
	require.Equal(t, genesis.Message, parsed.Message)
}

func TestGenesisWithValidators(t *testing.T) {
	// Test genesis with validators
	require := require.New(t)
	
	// Create a validator tx (simplified for testing)
	validatorTx := &txs.Tx{
		Unsigned: &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{},
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  0,
				End:    100,
				Wght:   1000,
			},
			RewardsOwner: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		},
	}
	
	err := validatorTx.Initialize(txs.GenesisCodec)
	require.NoError(err)
	
	genesis := &Genesis{
		UTXOs:         []*UTXO{},
		Validators:    []*txs.Tx{validatorTx},
		Chains:        []*txs.Tx{},
		Timestamp:     5,
		InitialSupply: 123456789,
		Message:       "Test Genesis with Validators",
	}
	
	// Encode and parse
	bytes, err := Codec.Marshal(CodecVersion, genesis)
	require.NoError(err)
	
	parsed, err := Parse(bytes)
	require.NoError(err)
	require.Len(parsed.Validators, 1)
}

func TestGenesisWithChains(t *testing.T) {
	// Test genesis with chains
	require := require.New(t)
	
	// Create a chain tx (simplified for testing)
	chainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			BaseTx: txs.BaseTx{
				BaseTx: lux.BaseTx{
					NetworkID:    1,
					Ins:          []*lux.TransferableInput{},
					Outs:         []*lux.TransferableOutput{},
				},
			},
			SubnetID:    ids.GenerateTestID(),
			ChainName:   "test chain",
			VMID:        ids.GenerateTestID(),
			FxIDs:       []ids.ID{},
			GenesisData: []byte("genesis data"),
			SubnetAuth:  &secp256k1fx.Input{},
		},
	}
	
	err := chainTx.Initialize(txs.GenesisCodec)
	require.NoError(err)
	
	genesis := &Genesis{
		UTXOs:         []*UTXO{},
		Validators:    []*txs.Tx{},
		Chains:        []*txs.Tx{chainTx},
		Timestamp:     5,
		InitialSupply: 123456789,
		Message:       "Test Genesis with Chains",
	}
	
	// Encode and parse
	bytes, err := Codec.Marshal(CodecVersion, genesis)
	require.NoError(err)
	
	parsed, err := Parse(bytes)
	require.NoError(err)
	require.Len(parsed.Chains, 1)
}

func TestGenesisUTXOMessage(t *testing.T) {
	// Test that UTXO messages are preserved
	require := require.New(t)
	
	message := []byte("important message")
	utxo := &UTXO{
		UTXO: lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: 0,
			},
			Asset: lux.Asset{
				ID: ids.GenerateTestID(),
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: 999999,
			},
		},
		Message: message,
	}
	
	genesis := &Genesis{
		UTXOs:         []*UTXO{utxo},
		Validators:    []*txs.Tx{},
		Chains:        []*txs.Tx{},
		Timestamp:     100,
		InitialSupply: 999999,
		Message:       "Genesis with UTXO message",
	}
	
	// Encode and parse
	bytes, err := Codec.Marshal(CodecVersion, genesis)
	require.NoError(err)
	
	parsed, err := Parse(bytes)
	require.NoError(err)
	require.Len(parsed.UTXOs, 1)
	require.Equal(message, parsed.UTXOs[0].Message)
}
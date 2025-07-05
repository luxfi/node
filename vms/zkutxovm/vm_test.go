// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zkutxovm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/utils/logging"
)

func TestVMInitialize(t *testing.T) {
	require := require.New(t)

	// Create test context
	ctx := context.Background()
	chainCtx := &snow.Context{
		ChainID: ids.GenerateTestID(),
		Log:     logging.NoLog{},
	}

	// Create test database
	db := memdb.New()

	// Create genesis
	genesis := &Genesis{
		Timestamp: 1607144400,
		InitialTxs: []*Transaction{
			{
				Type:    TransactionTypeMint,
				Outputs: []*ShieldedOutput{
					{
						Commitment:      make([]byte, 32),
						EncryptedNote:   make([]byte, 256),
						EphemeralPubKey: make([]byte, 32),
						OutputProof:     make([]byte, 128),
					},
				},
				Proof: &ZKProof{
					ProofType:    "groth16",
					ProofData:    make([]byte, 256),
					PublicInputs: [][]byte{make([]byte, 32)},
				},
			},
		},
	}

	genesisBytes, err := utils.Codec.Marshal(codecVersion, genesis)
	require.NoError(err)

	// Create config
	config := ZKConfig{
		EnableConfidentialTransfers: true,
		EnablePrivateAddresses:      true,
		ProofSystem:                 "groth16",
		CircuitType:                 "transfer",
		EnableFHE:                   false,
		MaxUTXOsPerBlock:            100,
		ProofCacheSize:              1000,
	}

	configBytes, err := utils.Codec.Marshal(codecVersion, config)
	require.NoError(err)

	// Create VM
	vm := &VM{}

	// Initialize VM
	toEngine := make(chan common.Message, 1)
	err = vm.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		nil, // upgradeBytes
		configBytes,
		toEngine,
		nil, // fxs
		nil, // appSender
	)
	require.NoError(err)

	// Verify initialization
	require.NotNil(vm.utxoDB)
	require.NotNil(vm.nullifierDB)
	require.NotNil(vm.stateTree)
	require.NotNil(vm.proofVerifier)
	require.NotNil(vm.addressManager)
	require.NotNil(vm.mempool)

	// Test health check
	health, err := vm.HealthCheck(ctx)
	require.NoError(err)
	require.NotNil(health)

	// Shutdown
	err = vm.Shutdown(ctx)
	require.NoError(err)
}

func TestShieldedTransaction(t *testing.T) {
	require := require.New(t)

	// Setup VM
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	// Create a shielded transaction
	tx := &Transaction{
		Type:    TransactionTypeTransfer,
		Version: 1,
		Nullifiers: [][]byte{
			make([]byte, 32), // dummy nullifier
		},
		Outputs: []*ShieldedOutput{
			{
				Commitment:      make([]byte, 32),
				EncryptedNote:   make([]byte, 256),
				EphemeralPubKey: make([]byte, 32),
				OutputProof:     make([]byte, 128),
			},
		},
		Proof: &ZKProof{
			ProofType: "groth16",
			ProofData: make([]byte, 256),
			PublicInputs: [][]byte{
				make([]byte, 32), // nullifier
				make([]byte, 32), // output commitment
			},
		},
		Fee:    1000,
		Expiry: 0,
	}

	// Compute transaction ID
	tx.ID = tx.ComputeID()

	// Validate transaction
	err := tx.ValidateBasic()
	require.NoError(err)

	// Add to mempool
	err = vm.mempool.AddTransaction(tx)
	require.NoError(err)

	// Verify in mempool
	require.True(vm.mempool.HasTransaction(tx.ID))
	require.Equal(1, vm.mempool.Size())
}

func TestPrivateAddress(t *testing.T) {
	require := require.New(t)

	// Setup VM with privacy enabled
	vm := setupTestVMWithPrivacy(t)
	defer vm.Shutdown(context.Background())

	// Generate a private address
	addr, err := vm.addressManager.GenerateAddress()
	require.NoError(err)
	require.NotNil(addr)

	// Verify address components
	require.Len(addr.Address, 32)
	require.Len(addr.ViewingKey, 32)
	require.Len(addr.SpendingKey, 32)
	require.Len(addr.Diversifier, 11)
	require.Len(addr.IncomingViewKey, 32)

	// Test address retrieval
	retrieved, err := vm.addressManager.GetAddress(addr.Address)
	require.NoError(err)
	require.Equal(addr.Address, retrieved.Address)
}

// Helper functions

func setupTestVM(t *testing.T) *VM {
	ctx := context.Background()
	chainCtx := &snow.Context{
		ChainID: ids.GenerateTestID(),
		Log:     logging.NoLog{},
	}

	db := memdb.New()

	genesis := &Genesis{Timestamp: 1607144400}
	genesisBytes, _ := utils.Codec.Marshal(codecVersion, genesis)

	config := ZKConfig{
		ProofSystem:      "groth16",
		MaxUTXOsPerBlock: 100,
		ProofCacheSize:   1000,
	}
	configBytes, _ := utils.Codec.Marshal(codecVersion, config)

	vm := &VM{}
	toEngine := make(chan common.Message, 1)

	err := vm.Initialize(ctx, chainCtx, db, genesisBytes, nil, configBytes, toEngine, nil, nil)
	require.NoError(t, err)

	return vm
}

func setupTestVMWithPrivacy(t *testing.T) *VM {
	ctx := context.Background()
	chainCtx := &snow.Context{
		ChainID: ids.GenerateTestID(),
		Log:     logging.NoLog{},
	}

	db := memdb.New()

	genesis := &Genesis{Timestamp: 1607144400}
	genesisBytes, _ := utils.Codec.Marshal(codecVersion, genesis)

	config := ZKConfig{
		EnablePrivateAddresses: true,
		ProofSystem:            "groth16",
		MaxUTXOsPerBlock:       100,
		ProofCacheSize:         1000,
	}
	configBytes, _ := utils.Codec.Marshal(codecVersion, config)

	vm := &VM{}
	toEngine := make(chan common.Message, 1)

	err := vm.Initialize(ctx, chainCtx, db, genesisBytes, nil, configBytes, toEngine, nil, nil)
	require.NoError(t, err)

	return vm
}
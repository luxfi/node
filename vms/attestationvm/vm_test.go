// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package attestationvm

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
		InitialOracles: []*OracleInfo{
			{
				ID:        "oracle1",
				Name:      "Test Oracle 1",
				PublicKey: []byte{1, 2, 3},
				Endpoint:  "https://oracle1.example.com",
				Feeds:     []string{"price", "weather"},
			},
		},
	}
	
	genesisBytes, err := utils.Codec.Marshal(codecVersion, genesis)
	require.NoError(err)
	
	// Create config
	config := AttestationConfig{
		SignatureThreshold:          2,
		MaxSigners:                  5,
		OracleRegistryEnabled:      true,
		TEEVerificationEnabled:     true,
		GPUProofVerificationEnabled: true,
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
	require.NotNil(vm.attestationDB)
	require.NotNil(vm.oracleRegistry)
	require.NotNil(vm.signatureVerifier)
	require.Equal(config.SignatureThreshold, vm.config.SignatureThreshold)
	
	// Test health check
	health, err := vm.HealthCheck(ctx)
	require.NoError(err)
	require.NotNil(health)
	
	// Shutdown
	err = vm.Shutdown(ctx)
	require.NoError(err)
}

func TestAttestationSubmission(t *testing.T) {
	require := require.New(t)
	
	// Setup VM
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())
	
	// Create test attestation
	att := &Attestation{
		Type:      AttestationTypeOracle,
		SourceID:  "oracle1",
		Data:      []byte("test data"),
		Timestamp: 1607144400,
		Signatures: [][]byte{
			make([]byte, 65),
			make([]byte, 65),
		},
		SignerIDs: []string{"signer1", "signer2"},
		Metadata:  []byte("metadata"),
	}
	
	// Submit attestation
	err := vm.SubmitAttestation(att)
	require.NoError(err)
	
	// Verify attestation was added to pending pool
	pendingAtts, err := vm.attestationDB.GetPendingAttestations(10)
	require.NoError(err)
	require.Len(pendingAtts, 1)
	require.Equal(att.Data, pendingAtts[0].Data)
}

// Helper function to setup test VM
func setupTestVM(t *testing.T) *VM {
	ctx := context.Background()
	chainCtx := &snow.Context{
		ChainID: ids.GenerateTestID(),
		Log:     logging.NoLog{},
	}
	
	db := memdb.New()
	
	genesis := &Genesis{Timestamp: 1607144400}
	genesisBytes, _ := utils.Codec.Marshal(codecVersion, genesis)
	
	config := AttestationConfig{
		SignatureThreshold:    2,
		OracleRegistryEnabled: true,
	}
	configBytes, _ := utils.Codec.Marshal(codecVersion, config)
	
	vm := &VM{}
	toEngine := make(chan common.Message, 1)
	
	err := vm.Initialize(ctx, chainCtx, db, genesisBytes, nil, configBytes, toEngine, nil, nil)
	require.NoError(t, err)
	
	return vm
}
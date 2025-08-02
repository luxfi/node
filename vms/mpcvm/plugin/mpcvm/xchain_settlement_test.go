// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
)

// Mock implementations for X-Chain settlement testing
type mockXChainClient struct {
	balance      uint64
	transactions []interface{}
	utxos        []UTXO
	mu           sync.Mutex
}

type UTXO struct {
	UTXOID ids.ID
	Amount uint64
}

func (m *mockXChainClient) GetBalance(address common.Address, assetID ids.ID) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balance, nil
}

func (m *mockXChainClient) GetUTXOs(address common.Address, assetID ids.ID, amount uint64) ([]UTXO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.balance < amount {
		return nil, fmt.Errorf("insufficient balance")
	}
	
	// Return mock UTXOs
	return []UTXO{
		{UTXOID: ids.GenerateID(), Amount: amount},
	}, nil
}

func (m *mockXChainClient) IssueTx(tx interface{}) (ids.ID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.transactions = append(m.transactions, tx)
	return ids.GenerateID(), nil
}

func (m *mockXChainClient) NetworkID() uint32 {
	return constants.MainnetID
}

type mockMPCWallet struct {
	signatures map[ids.ID][]byte
}

func (m *mockMPCWallet) SignXChainTx(tx interface{}) (interface{}, error) {
	txID := ids.GenerateID()
	m.signatures[txID] = []byte("mock_signature")
	return tx, nil
}

type mockZKProver struct{}

func (m *mockZKProver) VerifyProof(circuit string, proof []byte, publicInputs []byte) error {
	if len(proof) == 0 {
		return fmt.Errorf("empty proof")
	}
	return nil
}

func TestXChainSettlement_ProcessIncomingAssets(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 10000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     10,
		BatchInterval: 100 * time.Millisecond,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Test data
	sourceChain := ids.GenerateID()
	assetID := ids.GenerateID()
	amount := uint64(1000)
	recipient := common.HexToAddress("0x1234567890123456789012345678901234567890")
	
	proof := &TeleportProof{
		TransferProof:      []byte("transfer_proof"),
		SourceStateProof:   []byte("source_state_proof"),
		DestStateProof:     []byte("dest_state_proof"),
		AssetValidityProof: []byte("asset_validity_proof"),
		ProofType:          "groth16",
		GeneratedAt:        time.Now(),
	}
	
	// Process incoming assets (mint on X-Chain)
	ctx := context.Background()
	result, err := settlement.ProcessIncomingAssets(ctx, sourceChain, assetID, amount, recipient, proof)
	
	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, SettlementTypeMint, result.Type)
	assert.Equal(t, assetID, result.AssetID)
	assert.Equal(t, amount, result.Amount)
	assert.Equal(t, sourceChain, result.SourceChain)
	assert.Equal(t, constants.XVMID, result.DestChain)
	assert.Equal(t, recipient, result.Recipient)
	assert.Equal(t, SettlementStatusPending, result.Status)
	
	// Verify settlement was added to pending
	assert.NotNil(t, settlement.pendingSettlements[result.ID])
}

func TestXChainSettlement_ProcessOutgoingAssets(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 10000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     10,
		BatchInterval: 100 * time.Millisecond,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Test data
	destChain := ids.GenerateID()
	assetID := ids.GenerateID()
	amount := uint64(500)
	sender := common.HexToAddress("0x0987654321098765432109876543210987654321")
	
	proof := &TeleportProof{
		TransferProof:      []byte("transfer_proof"),
		SourceStateProof:   []byte("source_state_proof"),
		DestStateProof:     []byte("dest_state_proof"),
		AssetValidityProof: []byte("asset_validity_proof"),
		ProofType:          "groth16",
		GeneratedAt:        time.Now(),
	}
	
	// Process outgoing assets (burn on X-Chain)
	ctx := context.Background()
	result, err := settlement.ProcessOutgoingAssets(ctx, destChain, assetID, amount, sender, proof)
	
	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, SettlementTypeBurn, result.Type)
	assert.Equal(t, assetID, result.AssetID)
	assert.Equal(t, amount, result.Amount)
	assert.Equal(t, constants.XVMID, result.SourceChain)
	assert.Equal(t, destChain, result.DestChain)
	assert.Equal(t, sender, result.Recipient)
	assert.Equal(t, SettlementStatusPending, result.Status)
}

func TestXChainSettlement_ProcessOutgoingAssets_InsufficientBalance(t *testing.T) {
	// Setup with insufficient balance
	client := &mockXChainClient{balance: 100}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     10,
		BatchInterval: 100 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Try to burn more than available balance
	ctx := context.Background()
	_, err := settlement.ProcessOutgoingAssets(
		ctx,
		ids.GenerateID(),
		ids.GenerateID(),
		1000, // More than balance
		common.HexToAddress("0x1234567890123456789012345678901234567890"),
		&TeleportProof{
			TransferProof: []byte("proof"),
			ProofType:     "groth16",
		},
	)
	
	// Should fail due to insufficient balance
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance")
}

func TestXChainSettlement_BatchProcessing(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 100000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     3, // Small batch size for testing
		BatchInterval: 50 * time.Millisecond,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Create multiple settlements
	ctx := context.Background()
	numSettlements := 5
	settlements := make([]*Settlement, numSettlements)
	
	for i := 0; i < numSettlements; i++ {
		proof := &TeleportProof{
			TransferProof: []byte(fmt.Sprintf("proof_%d", i)),
			ProofType:     "groth16",
		}
		
		s, err := settlement.ProcessIncomingAssets(
			ctx,
			ids.GenerateID(),
			ids.GenerateID(),
			uint64(100*(i+1)),
			common.HexToAddress(fmt.Sprintf("0x%040d", i)),
			proof,
		)
		require.NoError(t, err)
		settlements[i] = s
	}
	
	// Wait for batch processing
	time.Sleep(200 * time.Millisecond)
	
	// Verify transactions were batched
	assert.Greater(t, len(client.transactions), 0)
	assert.LessOrEqual(t, len(client.transactions), 2) // Should be processed in 2 batches (3+2)
}

func TestXChainSettlement_ProofVerification(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 10000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     10,
		BatchInterval: 100 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	tests := []struct {
		name      string
		proof     *TeleportProof
		expectErr bool
	}{
		{
			name: "Valid proof",
			proof: &TeleportProof{
				TransferProof:      []byte("valid_transfer_proof"),
				SourceStateProof:   []byte("valid_source_proof"),
				DestStateProof:     []byte("valid_dest_proof"),
				AssetValidityProof: []byte("valid_asset_proof"),
				ProofType:          "groth16",
			},
			expectErr: false,
		},
		{
			name: "Empty transfer proof",
			proof: &TeleportProof{
				TransferProof:      []byte{}, // Empty
				SourceStateProof:   []byte("valid_source_proof"),
				DestStateProof:     []byte("valid_dest_proof"),
				AssetValidityProof: []byte("valid_asset_proof"),
				ProofType:          "groth16",
			},
			expectErr: true,
		},
		{
			name: "Empty source state proof",
			proof: &TeleportProof{
				TransferProof:      []byte("valid_transfer_proof"),
				SourceStateProof:   []byte{}, // Empty
				DestStateProof:     []byte("valid_dest_proof"),
				AssetValidityProof: []byte("valid_asset_proof"),
				ProofType:          "groth16",
			},
			expectErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := settlement.verifyTeleportProof(tt.proof)
			
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestXChainSettlement_ConcurrentSettlements(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 1000000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     10,
		BatchInterval: 100 * time.Millisecond,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Process many settlements concurrently
	ctx := context.Background()
	numSettlements := 50
	var wg sync.WaitGroup
	errors := make(chan error, numSettlements)
	
	for i := 0; i < numSettlements; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			proof := &TeleportProof{
				TransferProof:      []byte(fmt.Sprintf("proof_%d", index)),
				SourceStateProof:   []byte("source_proof"),
				DestStateProof:     []byte("dest_proof"),
				AssetValidityProof: []byte("asset_proof"),
				ProofType:          "groth16",
			}
			
			var err error
			if index%2 == 0 {
				// Mint operation
				_, err = settlement.ProcessIncomingAssets(
					ctx,
					ids.GenerateID(),
					ids.GenerateID(),
					uint64(100+index),
					common.HexToAddress(fmt.Sprintf("0x%040d", index)),
					proof,
				)
			} else {
				// Burn operation
				_, err = settlement.ProcessOutgoingAssets(
					ctx,
					ids.GenerateID(),
					ids.GenerateID(),
					uint64(50+index),
					common.HexToAddress(fmt.Sprintf("0x%040d", index)),
					proof,
				)
			}
			
			if err != nil {
				errors <- err
			}
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent settlement failed: %v", err)
		errorCount++
	}
	
	assert.Equal(t, 0, errorCount, "Some concurrent settlements failed")
	
	// Wait for batch processing to complete
	time.Sleep(500 * time.Millisecond)
	
	// Verify settlements were processed
	assert.Greater(t, len(client.transactions), 0)
}

func TestXChainSettlement_GetPendingSettlements(t *testing.T) {
	// Setup
	client := &mockXChainClient{balance: 10000}
	mpcWallet := &mockMPCWallet{signatures: make(map[ids.ID][]byte)}
	zkProver := &mockZKProver{}
	
	config := XChainSettlementConfig{
		BatchSize:     100, // Large batch to prevent immediate processing
		BatchInterval: 10 * time.Second,
	}
	
	settlement := NewXChainSettlement(client, mpcWallet, zkProver, config)
	
	// Add settlements
	ctx := context.Background()
	numSettlements := 5
	
	for i := 0; i < numSettlements; i++ {
		proof := &TeleportProof{
			TransferProof: []byte(fmt.Sprintf("proof_%d", i)),
			ProofType:     "groth16",
		}
		
		_, err := settlement.ProcessIncomingAssets(
			ctx,
			ids.GenerateID(),
			ids.GenerateID(),
			uint64(100*(i+1)),
			common.HexToAddress(fmt.Sprintf("0x%040d", i)),
			proof,
		)
		require.NoError(t, err)
	}
	
	// Get pending settlements
	pending := settlement.GetPendingSettlements()
	
	// Verify all settlements are pending
	assert.Len(t, pending, numSettlements)
	
	for _, s := range pending {
		assert.Equal(t, SettlementStatusPending, s.Status)
	}
}
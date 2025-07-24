// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mvm

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
)

// Mock implementations for testing
type mockIntentPool struct {
	intents []*TeleportIntent
}

func (m *mockIntentPool) AddIntent(intent *TeleportIntent) error {
	m.intents = append(m.intents, intent)
	return nil
}

func (m *mockIntentPool) GetPendingIntents(limit int) []*TeleportIntent {
	if len(m.intents) <= limit {
		return m.intents
	}
	return m.intents[:limit]
}

func (m *mockIntentPool) RemoveIntent(id ids.ID) {
	for i, intent := range m.intents {
		if intent.ID == id {
			m.intents = append(m.intents[:i], m.intents[i+1:]...)
			break
		}
	}
}

func (m *mockIntentPool) Len() int {
	return len(m.intents)
}

type mockAssetRegistry struct {
	assets map[ids.ID]*TeleportAsset
}

func newMockAssetRegistry() *mockAssetRegistry {
	return &mockAssetRegistry{
		assets: make(map[ids.ID]*TeleportAsset),
	}
}

func (m *mockAssetRegistry) GetAsset(id ids.ID) (*TeleportAsset, error) {
	asset, exists := m.assets[id]
	if !exists {
		return nil, fmt.Errorf("asset not found")
	}
	return asset, nil
}

func (m *mockAssetRegistry) UpdateAssetLocation(id ids.ID, chainID ids.ID) error {
	if asset, exists := m.assets[id]; exists {
		asset.CurrentChain = chainID
	}
	return nil
}

func (m *mockAssetRegistry) RegisterAsset(asset *TeleportAsset) error {
	m.assets[asset.ID] = asset
	return nil
}

type mockXChainSettlement struct {
	settlements []*Settlement
}

func (m *mockXChainSettlement) ProcessIncomingAssets(
	ctx context.Context,
	sourceChain ids.ID,
	assetID ids.ID,
	amount uint64,
	recipient common.Address,
	proof *TeleportProof,
) (*Settlement, error) {
	settlement := &Settlement{
		ID:          ids.GenerateID(),
		Type:        SettlementTypeMint,
		AssetID:     assetID,
		Amount:      amount,
		SourceChain: sourceChain,
		DestChain:   constants.XVMID,
		Recipient:   recipient,
		Status:      SettlementStatusCompleted,
		CreatedAt:   time.Now(),
	}
	m.settlements = append(m.settlements, settlement)
	return settlement, nil
}

func (m *mockXChainSettlement) ProcessOutgoingAssets(
	ctx context.Context,
	destChain ids.ID,
	assetID ids.ID,
	amount uint64,
	sender common.Address,
	proof *TeleportProof,
) (*Settlement, error) {
	settlement := &Settlement{
		ID:          ids.GenerateID(),
		Type:        SettlementTypeBurn,
		AssetID:     assetID,
		Amount:      amount,
		SourceChain: constants.XVMID,
		DestChain:   destChain,
		Recipient:   sender,
		Status:      SettlementStatusCompleted,
		CreatedAt:   time.Now(),
	}
	m.settlements = append(m.settlements, settlement)
	return settlement, nil
}

func TestTeleportEngine_ProcessIntent_FungibleTransfer(t *testing.T) {
	// Setup
	intentPool := &mockIntentPool{}
	assetRegistry := newMockAssetRegistry()
	xchainSettlement := &mockXChainSettlement{}
	
	engine := &TeleportEngine{
		intentPool:       intentPool,
		assetRegistry:    assetRegistry,
		xchainSettlement: xchainSettlement,
		zkVerifier:       &ZKVerifier{},
		executorEngine:   &ExecutorEngine{},
		activeTransfers:  make(map[ids.ID]*TeleportTransfer),
		totalVolume:      big.NewInt(0),
		config:           TeleportConfig{},
	}
	
	// Register test asset
	testAsset := &TeleportAsset{
		ID:           ids.GenerateID(),
		Type:         AssetTypeFungible,
		OriginChain:  ids.GenerateID(),
		CurrentChain: ids.GenerateID(),
		TotalSupply:  big.NewInt(1000000),
		Decimals:     18,
		Metadata: AssetMetadata{
			Name:   "Test Token",
			Symbol: "TEST",
		},
	}
	assetRegistry.RegisterAsset(testAsset)
	
	// Create intent
	intent := &TeleportIntent{
		ID:         ids.GenerateID(),
		IntentType: IntentTypeTransfer,
		SourceAsset: AssetIdentifier{
			ChainID: testAsset.CurrentChain,
			AssetID: testAsset.ID,
		},
		DestAsset: AssetIdentifier{
			ChainID: constants.EVMID,
			AssetID: testAsset.ID,
		},
		Amount:    big.NewInt(1000),
		Sender:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Recipient: common.HexToAddress("0x0987654321098765432109876543210987654321"),
		Deadline:  time.Now().Add(5 * time.Minute),
		Signature: []byte("test_signature"),
	}
	
	// Process intent
	ctx := context.Background()
	transfer, err := engine.ProcessIntent(ctx, intent)
	
	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, transfer)
	assert.Equal(t, intent.ID, transfer.Intent.ID)
	assert.Equal(t, testAsset.ID, transfer.Asset.ID)
	assert.Equal(t, TransferStatusCompleted, transfer.Status)
	assert.Equal(t, 0, new(big.Int).Cmp(transfer.Amount, intent.Amount))
	
	// Verify settlement was created
	assert.Len(t, xchainSettlement.settlements, 1)
	settlement := xchainSettlement.settlements[0]
	assert.Equal(t, testAsset.ID, settlement.AssetID)
	assert.Equal(t, uint64(1000), settlement.Amount)
}

func TestTeleportEngine_ProcessIntent_NFTTransfer(t *testing.T) {
	// Setup
	intentPool := &mockIntentPool{}
	assetRegistry := newMockAssetRegistry()
	xchainSettlement := &mockXChainSettlement{}
	
	engine := &TeleportEngine{
		intentPool:       intentPool,
		assetRegistry:    assetRegistry,
		xchainSettlement: xchainSettlement,
		zkVerifier:       &ZKVerifier{},
		executorEngine:   &ExecutorEngine{},
		activeTransfers:  make(map[ids.ID]*TeleportTransfer),
		totalVolume:      big.NewInt(0),
		nftHandler:       &NFTHandler{validatorNFTs: make(map[ids.ID]*ValidatorNFT)},
		config:           TeleportConfig{},
	}
	
	// Register test NFT
	testNFT := &TeleportAsset{
		ID:              ids.GenerateID(),
		Type:            AssetTypeNFT,
		OriginChain:     constants.EVMID,
		CurrentChain:    constants.EVMID,
		ContractAddress: common.HexToAddress("0xNFTContract"),
		TokenID:         big.NewInt(42),
		Metadata: AssetMetadata{
			Name:        "Test NFT",
			Symbol:      "TNFT",
			Description: "A test NFT",
			ImageURI:    "ipfs://QmTest",
		},
	}
	assetRegistry.RegisterAsset(testNFT)
	
	// Test case 1: C-Chain to X-Chain NFT transfer
	t.Run("CChain_to_XChain", func(t *testing.T) {
		intent := &TeleportIntent{
			ID:         ids.GenerateID(),
			IntentType: IntentTypeTransfer,
			SourceAsset: AssetIdentifier{
				ChainID:         constants.EVMID,
				AssetID:         testNFT.ID,
				ContractAddress: testNFT.ContractAddress,
				TokenID:         testNFT.TokenID,
			},
			DestAsset: AssetIdentifier{
				ChainID: constants.XVMID,
				AssetID: testNFT.ID,
			},
			Amount:    big.NewInt(1), // NFTs have quantity 1
			Sender:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Recipient: common.HexToAddress("0x0987654321098765432109876543210987654321"),
			Deadline:  time.Now().Add(5 * time.Minute),
			Signature: []byte("test_signature"),
		}
		
		ctx := context.Background()
		transfer, err := engine.ProcessIntent(ctx, intent)
		
		require.NoError(t, err)
		assert.NotNil(t, transfer)
		assert.Equal(t, AssetTypeNFT, transfer.Asset.Type)
		assert.Equal(t, TransferStatusCompleted, transfer.Status)
		
		// Verify asset location was updated
		assert.Equal(t, constants.XVMID, testNFT.CurrentChain)
	})
	
	// Test case 2: X-Chain to C-Chain NFT transfer
	t.Run("XChain_to_CChain", func(t *testing.T) {
		// Update NFT location to X-Chain
		testNFT.CurrentChain = constants.XVMID
		
		intent := &TeleportIntent{
			ID:         ids.GenerateID(),
			IntentType: IntentTypeTransfer,
			SourceAsset: AssetIdentifier{
				ChainID: constants.XVMID,
				AssetID: testNFT.ID,
			},
			DestAsset: AssetIdentifier{
				ChainID:         constants.EVMID,
				AssetID:         testNFT.ID,
				ContractAddress: testNFT.ContractAddress,
			},
			Amount:    big.NewInt(1),
			Sender:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
			Recipient: common.HexToAddress("0x0987654321098765432109876543210987654321"),
			Deadline:  time.Now().Add(5 * time.Minute),
			Signature: []byte("test_signature"),
		}
		
		ctx := context.Background()
		transfer, err := engine.ProcessIntent(ctx, intent)
		
		require.NoError(t, err)
		assert.NotNil(t, transfer)
		assert.Equal(t, AssetTypeNFT, transfer.Asset.Type)
		assert.Equal(t, TransferStatusCompleted, transfer.Status)
		
		// Verify asset location was updated
		assert.Equal(t, constants.EVMID, testNFT.CurrentChain)
	})
}

func TestTeleportEngine_ProcessIntent_ValidatorNFT(t *testing.T) {
	// Setup
	intentPool := &mockIntentPool{}
	assetRegistry := newMockAssetRegistry()
	xchainSettlement := &mockXChainSettlement{}
	
	nftHandler := &NFTHandler{
		validatorNFTs: make(map[ids.ID]*ValidatorNFT),
	}
	
	engine := &TeleportEngine{
		intentPool:       intentPool,
		assetRegistry:    assetRegistry,
		xchainSettlement: xchainSettlement,
		zkVerifier:       &ZKVerifier{},
		executorEngine:   &ExecutorEngine{},
		activeTransfers:  make(map[ids.ID]*TeleportTransfer),
		totalVolume:      big.NewInt(0),
		nftHandler:       nftHandler,
		config:           TeleportConfig{},
	}
	
	// Register validator NFT
	validatorNFT := &TeleportAsset{
		ID:              ids.GenerateID(),
		Type:            AssetTypeValidatorNFT,
		OriginChain:     constants.EVMID,
		CurrentChain:    constants.EVMID,
		ContractAddress: common.HexToAddress("0xValidatorNFT"),
		TokenID:         big.NewInt(1),
		Metadata: AssetMetadata{
			Name:           "Lux Validator NFT",
			Symbol:         "LUXVAL",
			Description:    "NFT that enables validator operation",
			ValidatorPower: big.NewInt(1000000), // 1M LUX equivalent
		},
	}
	assetRegistry.RegisterAsset(validatorNFT)
	
	// Register in NFT handler
	nftHandler.validatorNFTs[validatorNFT.ID] = &ValidatorNFT{
		NFTAssetID:      validatorNFT.ID,
		ValidatorNodeID: ids.GenerateNodeID(),
		StakeAmount:     1000000,
		StakeStartTime:  time.Now().Add(1 * time.Hour),
		StakeEndTime:    time.Now().Add(30 * 24 * time.Hour),
		DelegationFee:   200, // 2%
		Active:          false,
	}
	
	// Create stake intent
	intent := &TeleportIntent{
		ID:         ids.GenerateID(),
		IntentType: IntentTypeStake,
		SourceAsset: AssetIdentifier{
			ChainID:         constants.EVMID,
			AssetID:         validatorNFT.ID,
			ContractAddress: validatorNFT.ContractAddress,
			TokenID:         validatorNFT.TokenID,
		},
		DestAsset: AssetIdentifier{
			ChainID: constants.PlatformVMID,
			AssetID: validatorNFT.ID,
		},
		Amount:    big.NewInt(1),
		Sender:    common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Recipient: common.HexToAddress("0x1234567890123456789012345678901234567890"), // Same as sender for staking
		Deadline:  time.Now().Add(5 * time.Minute),
		Signature: []byte("test_signature"),
	}
	
	// Process intent
	ctx := context.Background()
	transfer, err := engine.ProcessIntent(ctx, intent)
	
	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, transfer)
	assert.Equal(t, AssetTypeValidatorNFT, transfer.Asset.Type)
	assert.Equal(t, TransferStatusCompleted, transfer.Status)
	assert.Equal(t, constants.PlatformVMID, transfer.DestChain)
	
	// Verify NFT is marked as active
	vnft, _ := nftHandler.GetValidatorNFT(validatorNFT.ID)
	assert.True(t, vnft.Active)
}

func TestTeleportEngine_ValidateIntent(t *testing.T) {
	assetRegistry := newMockAssetRegistry()
	
	engine := &TeleportEngine{
		assetRegistry:   assetRegistry,
		activeTransfers: make(map[ids.ID]*TeleportTransfer),
	}
	
	// Register test asset
	testAsset := &TeleportAsset{
		ID:   ids.GenerateID(),
		Type: AssetTypeFungible,
	}
	assetRegistry.RegisterAsset(testAsset)
	
	tests := []struct {
		name      string
		intent    *TeleportIntent
		expectErr bool
		errMsg    string
	}{
		{
			name: "Valid intent",
			intent: &TeleportIntent{
				ID:         ids.GenerateID(),
				IntentType: IntentTypeTransfer,
				SourceAsset: AssetIdentifier{
					AssetID: testAsset.ID,
				},
				Amount:    big.NewInt(100),
				Deadline:  time.Now().Add(5 * time.Minute),
				Signature: []byte("valid_signature"),
			},
			expectErr: false,
		},
		{
			name: "Expired intent",
			intent: &TeleportIntent{
				ID:         ids.GenerateID(),
				IntentType: IntentTypeTransfer,
				SourceAsset: AssetIdentifier{
					AssetID: testAsset.ID,
				},
				Amount:    big.NewInt(100),
				Deadline:  time.Now().Add(-5 * time.Minute), // Past deadline
				Signature: []byte("valid_signature"),
			},
			expectErr: true,
			errMsg:    "intent expired",
		},
		{
			name: "Zero amount",
			intent: &TeleportIntent{
				ID:         ids.GenerateID(),
				IntentType: IntentTypeTransfer,
				SourceAsset: AssetIdentifier{
					AssetID: testAsset.ID,
				},
				Amount:    big.NewInt(0),
				Deadline:  time.Now().Add(5 * time.Minute),
				Signature: []byte("valid_signature"),
			},
			expectErr: true,
			errMsg:    "invalid amount",
		},
		{
			name: "Unknown asset",
			intent: &TeleportIntent{
				ID:         ids.GenerateID(),
				IntentType: IntentTypeTransfer,
				SourceAsset: AssetIdentifier{
					AssetID: ids.GenerateID(), // Unknown asset
				},
				Amount:    big.NewInt(100),
				Deadline:  time.Now().Add(5 * time.Minute),
				Signature: []byte("valid_signature"),
			},
			expectErr: true,
			errMsg:    "source asset not found",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.validateIntent(tt.intent)
			
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTeleportEngine_ConcurrentTransfers(t *testing.T) {
	// Setup
	intentPool := &mockIntentPool{}
	assetRegistry := newMockAssetRegistry()
	xchainSettlement := &mockXChainSettlement{}
	
	engine := &TeleportEngine{
		intentPool:       intentPool,
		assetRegistry:    assetRegistry,
		xchainSettlement: xchainSettlement,
		zkVerifier:       &ZKVerifier{},
		executorEngine:   &ExecutorEngine{},
		activeTransfers:  make(map[ids.ID]*TeleportTransfer),
		totalVolume:      big.NewInt(0),
		config:           TeleportConfig{},
	}
	
	// Register multiple assets
	numAssets := 10
	for i := 0; i < numAssets; i++ {
		asset := &TeleportAsset{
			ID:           ids.GenerateID(),
			Type:         AssetTypeFungible,
			OriginChain:  ids.GenerateID(),
			CurrentChain: ids.GenerateID(),
			TotalSupply:  big.NewInt(1000000),
			Decimals:     18,
			Metadata: AssetMetadata{
				Name:   fmt.Sprintf("Test Token %d", i),
				Symbol: fmt.Sprintf("TEST%d", i),
			},
		}
		assetRegistry.RegisterAsset(asset)
	}
	
	// Process multiple intents concurrently
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, numAssets)
	
	for i := 0; i < numAssets; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			assetID := assetRegistry.assets[ids.ID{}].ID // Get any asset
			for id, asset := range assetRegistry.assets {
				if asset.Metadata.Symbol == fmt.Sprintf("TEST%d", index) {
					assetID = id
					break
				}
			}
			
			intent := &TeleportIntent{
				ID:         ids.GenerateID(),
				IntentType: IntentTypeTransfer,
				SourceAsset: AssetIdentifier{
					ChainID: ids.GenerateID(),
					AssetID: assetID,
				},
				DestAsset: AssetIdentifier{
					ChainID: constants.EVMID,
					AssetID: assetID,
				},
				Amount:    big.NewInt(int64(1000 * (index + 1))),
				Sender:    common.HexToAddress(fmt.Sprintf("0x%040d", index)),
				Recipient: common.HexToAddress(fmt.Sprintf("0x%040d", index+1000)),
				Deadline:  time.Now().Add(5 * time.Minute),
				Signature: []byte("test_signature"),
			}
			
			_, err := engine.ProcessIntent(ctx, intent)
			if err != nil {
				errors <- err
			}
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent transfer failed: %v", err)
	}
	
	// Verify all transfers completed
	assert.Equal(t, uint64(numAssets), engine.totalTransfers)
	
	// Verify total volume
	expectedVolume := big.NewInt(0)
	for i := 1; i <= numAssets; i++ {
		expectedVolume.Add(expectedVolume, big.NewInt(int64(1000*i)))
	}
	assert.Equal(t, 0, expectedVolume.Cmp(engine.totalVolume))
}
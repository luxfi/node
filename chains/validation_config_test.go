// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
)

// MockNFTVerifier for testing
type MockNFTVerifier struct {
	// Map of validator -> contract -> token IDs they own
	ownership map[ids.NodeID]map[string][]uint64
}

func NewMockNFTVerifier() *MockNFTVerifier {
	return &MockNFTVerifier{
		ownership: make(map[ids.NodeID]map[string][]uint64),
	}
}

func (m *MockNFTVerifier) VerifyNFTOwnership(validatorID ids.NodeID, contractAddress string, requiredTokenIDs []uint64) (bool, error) {
	contracts, exists := m.ownership[validatorID]
	if !exists {
		return false, nil
	}
	
	tokens, exists := contracts[contractAddress]
	if !exists {
		return false, nil
	}
	
	// Check if validator owns all required tokens
	tokenMap := make(map[uint64]bool)
	for _, token := range tokens {
		tokenMap[token] = true
	}
	
	for _, required := range requiredTokenIDs {
		if !tokenMap[required] {
			return false, nil
		}
	}
	
	return true, nil
}

func (m *MockNFTVerifier) SetOwnership(validatorID ids.NodeID, contractAddress string, tokenIDs []uint64) {
	if m.ownership[validatorID] == nil {
		m.ownership[validatorID] = make(map[string][]uint64)
	}
	m.ownership[validatorID][contractAddress] = tokenIDs
}

// MockStakingManager for testing
type MockStakingManager struct {
	stakes map[ids.NodeID]map[ids.ID]uint64
}

func NewMockStakingManager() *MockStakingManager {
	return &MockStakingManager{
		stakes: make(map[ids.NodeID]map[ids.ID]uint64),
	}
}

func (m *MockStakingManager) GetValidatorStake(validatorID ids.NodeID, chainID ids.ID) (uint64, error) {
	chains, exists := m.stakes[validatorID]
	if !exists {
		return 0, nil
	}
	return chains[chainID], nil
}

func (m *MockStakingManager) SetStake(validatorID ids.NodeID, chainID ids.ID, amount uint64) {
	if m.stakes[validatorID] == nil {
		m.stakes[validatorID] = make(map[ids.ID]uint64)
	}
	m.stakes[validatorID][chainID] = amount
}

// Test basic opt-in validation
func TestBasicOptInValidation(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	validatorID := ids.GenerateTestNodeID()
	
	// Set up stakes for basic chains
	stakingManager.SetStake(validatorID, constants.PlatformChainID, 2000)
	stakingManager.SetStake(validatorID, constants.QuantumChainID, 2000)
	stakingManager.SetStake(validatorID, constants.CChainID, 1000)
	stakingManager.SetStake(validatorID, constants.XChainID, 1000)
	
	// Basic configuration (C,P,Q,X)
	config := &ChainValidationConfig{
		ValidateCChain: true,
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateXChain: true,
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	
	// Register validator
	err := vcm.RegisterValidator(validatorID, config)
	require.NoError(err)
	
	// Check validator can validate opted-in chains
	require.True(vcm.CanValidateChain(validatorID, constants.CChainID))
	require.True(vcm.CanValidateChain(validatorID, constants.PlatformChainID))
	require.True(vcm.CanValidateChain(validatorID, constants.QuantumChainID))
	require.True(vcm.CanValidateChain(validatorID, constants.XChainID))
	
	// Check validator cannot validate non-opted-in chains
	require.False(vcm.CanValidateChain(validatorID, constants.AIChainID))
	require.False(vcm.CanValidateChain(validatorID, constants.BridgeChainID))
	require.False(vcm.CanValidateChain(validatorID, constants.MPCChainID))
	require.False(vcm.CanValidateChain(validatorID, constants.ZKChainID))
}

// Test Genesis NFT-gated chains
func TestGenesisNFTGatedChains(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	validatorID := ids.GenerateTestNodeID()
	genesisNFTContract := "0x1234567890abcdef"
	
	// Set up Genesis NFT ownership
	nftVerifier.SetOwnership(validatorID, genesisNFTContract, []uint64{42, 1001})
	
	// Set up stakes
	stakingManager.SetStake(validatorID, constants.PlatformChainID, 2000)
	stakingManager.SetStake(validatorID, constants.QuantumChainID, 2000)
	stakingManager.SetStake(validatorID, constants.BridgeChainID, 3000)
	stakingManager.SetStake(validatorID, constants.MPCChainID, 10000)
	
	// Config with B/M chains (requires Genesis NFT)
	config := &ChainValidationConfig{
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateBChain: true,
		ValidateMChain: true,
		GenesisNFTContract: genesisNFTContract,
		GenesisNFTTokenIDs: []uint64{42},
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	
	// Should succeed with Genesis NFT
	err := vcm.RegisterValidator(validatorID, config)
	require.NoError(err)
	
	// Verify validator can validate Genesis-gated chains
	require.True(vcm.CanValidateChain(validatorID, constants.BridgeChainID))
	require.True(vcm.CanValidateChain(validatorID, constants.MPCChainID))
	
	// Test validator without Genesis NFT
	validatorID2 := ids.GenerateTestNodeID()
	stakingManager.SetStake(validatorID2, constants.PlatformChainID, 2000)
	stakingManager.SetStake(validatorID2, constants.QuantumChainID, 2000)
	stakingManager.SetStake(validatorID2, constants.BridgeChainID, 3000)
	stakingManager.SetStake(validatorID2, constants.MPCChainID, 10000)
	
	// Should fail without Genesis NFT
	err = vcm.RegisterValidator(validatorID2, config)
	require.Error(err)
	require.Contains(err.Error(), "Genesis NFTs")
}

// Test staking requirements
func TestStakingRequirements(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	validatorID := ids.GenerateTestNodeID()
	
	// Insufficient stake for P-Chain
	stakingManager.SetStake(validatorID, constants.PlatformChainID, 1000) // Need 2000
	stakingManager.SetStake(validatorID, constants.QuantumChainID, 2000)
	
	config := &ChainValidationConfig{
		ValidatePChain: true,
		ValidateQChain: true,
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	
	// Should fail due to insufficient stake
	err := vcm.RegisterValidator(validatorID, config)
	require.Error(err)
	require.Contains(err.Error(), "insufficient stake")
	
	// Fix stake
	stakingManager.SetStake(validatorID, constants.PlatformChainID, 2000)
	
	// Should succeed now
	err = vcm.RegisterValidator(validatorID, config)
	require.NoError(err)
}

// Test full stack validator configuration
func TestFullStackValidator(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	validatorID := ids.GenerateTestNodeID()
	genesisNFTContract := "0x1234567890abcdef"
	
	// Set up Genesis NFT ownership
	nftVerifier.SetOwnership(validatorID, genesisNFTContract, []uint64{1}) // Founder NFT
	
	// Set up stakes for all chains
	stakingManager.SetStake(validatorID, constants.PlatformChainID, 2000)
	stakingManager.SetStake(validatorID, constants.QuantumChainID, 2000)
	stakingManager.SetStake(validatorID, constants.CChainID, 1000)
	stakingManager.SetStake(validatorID, constants.XChainID, 1000)
	stakingManager.SetStake(validatorID, constants.AIChainID, 5000)
	stakingManager.SetStake(validatorID, constants.BridgeChainID, 3000)
	stakingManager.SetStake(validatorID, constants.MPCChainID, 10000)
	stakingManager.SetStake(validatorID, constants.ZKChainID, 5000)
	
	// Full stack configuration
	config := &ChainValidationConfig{
		ValidateAChain: true,
		ValidateBChain: true,
		ValidateCChain: true,
		ValidateMChain: true,
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateXChain: true,
		ValidateZChain: true,
		GenesisNFTContract: genesisNFTContract,
		GenesisNFTTokenIDs: []uint64{1},
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	
	// Should succeed - validator has everything needed
	err := vcm.RegisterValidator(validatorID, config)
	require.NoError(err)
	
	// Verify validator can validate all chains
	for _, chainID := range []ids.ID{
		constants.AIChainID,
		constants.BridgeChainID,
		constants.CChainID,
		constants.MPCChainID,
		constants.PlatformChainID,
		constants.QuantumChainID,
		constants.XChainID,
		constants.ZKChainID,
	} {
		require.True(vcm.CanValidateChain(validatorID, chainID))
	}
}

// Test validator set retrieval
func TestGetValidatorsForChain(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	// Register multiple validators with different configurations
	genesisNFTContract := "0x1234567890abcdef"
	
	// Validator 1: Basic (C,P,Q,X)
	v1 := ids.GenerateTestNodeID()
	stakingManager.SetStake(v1, constants.PlatformChainID, 2000)
	stakingManager.SetStake(v1, constants.QuantumChainID, 2000)
	stakingManager.SetStake(v1, constants.CChainID, 1000)
	stakingManager.SetStake(v1, constants.XChainID, 1000)
	
	config1 := &ChainValidationConfig{
		ValidateCChain: true,
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateXChain: true,
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	require.NoError(vcm.RegisterValidator(v1, config1))
	
	// Validator 2: Genesis NFT holder (B,M,P,Q)
	v2 := ids.GenerateTestNodeID()
	nftVerifier.SetOwnership(v2, genesisNFTContract, []uint64{1001})
	stakingManager.SetStake(v2, constants.PlatformChainID, 2000)
	stakingManager.SetStake(v2, constants.QuantumChainID, 2000)
	stakingManager.SetStake(v2, constants.BridgeChainID, 3000)
	stakingManager.SetStake(v2, constants.MPCChainID, 10000)
	
	config2 := &ChainValidationConfig{
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateBChain: true,
		ValidateMChain: true,
		GenesisNFTContract: genesisNFTContract,
		GenesisNFTTokenIDs: []uint64{1001},
		MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
	}
	require.NoError(vcm.RegisterValidator(v2, config2))
	
	// Check validator sets
	pValidators := vcm.GetValidatorsForChain(constants.PlatformChainID)
	require.Len(pValidators, 2) // Both validators
	
	cValidators := vcm.GetValidatorsForChain(constants.CChainID)
	require.Len(cValidators, 1) // Only v1
	require.Contains(cValidators, v1)
	
	bValidators := vcm.GetValidatorsForChain(constants.BridgeChainID)
	require.Len(bValidators, 1) // Only v2 (Genesis NFT holder)
	require.Contains(bValidators, v2)
	
	mValidators := vcm.GetValidatorsForChain(constants.MPCChainID)
	require.Len(mValidators, 1) // Only v2 (Genesis NFT holder)
	require.Contains(mValidators, v2)
}

// Test chain validator set requirements
func TestChainValidatorSet(t *testing.T) {
	require := require.New(t)
	
	nftVerifier := NewMockNFTVerifier()
	stakingManager := NewMockStakingManager()
	vcm := NewValidatorChainManager(nftVerifier, stakingManager)
	
	// Test P-Chain validator set
	pSet, err := vcm.GetChainValidatorSet(constants.PlatformChainID)
	require.Error(err) // No validators yet
	
	// Add validators
	for i := 0; i < 21; i++ {
		v := ids.GenerateTestNodeID()
		stakingManager.SetStake(v, constants.PlatformChainID, 2000)
		stakingManager.SetStake(v, constants.QuantumChainID, 2000)
		
		config := &ChainValidationConfig{
			ValidatePChain: true,
			ValidateQChain: true,
			MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
		}
		require.NoError(vcm.RegisterValidator(v, config))
	}
	
	// Now should succeed
	pSet, err = vcm.GetChainValidatorSet(constants.PlatformChainID)
	require.NoError(err)
	require.Len(pSet.Validators, 21)
	require.Equal(21, pSet.MinValidators)
	require.False(pSet.IsGenesisGated)
	
	// Test B-Chain (Genesis NFT-gated)
	genesisNFTContract := "0x1234567890abcdef"
	for i := 0; i < 7; i++ {
		v := ids.GenerateTestNodeID()
		nftVerifier.SetOwnership(v, genesisNFTContract, []uint64{uint64(1001 + i)})
		stakingManager.SetStake(v, constants.PlatformChainID, 2000)
		stakingManager.SetStake(v, constants.QuantumChainID, 2000)
		stakingManager.SetStake(v, constants.BridgeChainID, 3000)
		
		config := &ChainValidationConfig{
			ValidatePChain: true,
			ValidateQChain: true,
			ValidateBChain: true,
			GenesisNFTContract: genesisNFTContract,
			GenesisNFTTokenIDs: []uint64{uint64(1001 + i)},
			MinStakePerChain: DefaultValidationConfig().MinStakePerChain,
		}
		require.NoError(vcm.RegisterValidator(v, config))
	}
	
	bSet, err := vcm.GetChainValidatorSet(constants.BridgeChainID)
	require.NoError(err)
	require.Len(bSet.Validators, 7)
	require.Equal(7, bSet.MinValidators)
	require.True(bSet.IsGenesisGated)
}
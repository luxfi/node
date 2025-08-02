// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
)

// ChainValidationConfig specifies which chains a validator participates in
type ChainValidationConfig struct {
	// Core chains that validators can opt into
	ValidateAChain bool // AI Chain (opt-in)
	ValidateBChain bool // Bridge Chain (Genesis NFT-gated)
	ValidateCChain bool // EVM Chain
	ValidateMChain bool // MPC Chain (Genesis NFT-gated)
	ValidatePChain bool // Platform Chain (usually required)
	ValidateQChain bool // Quantum Chain (usually required)
	ValidateXChain bool // Exchange Chain
	ValidateZChain bool // ZK Chain (opt-in)

	// Genesis NFT requirements for B/M chains
	GenesisNFTContract string   // Address of Genesis NFT contract
	GenesisNFTTokenIDs []uint64 // Required Genesis NFT token IDs
	
	// Optional specific NFT requirements per chain
	ChainSpecificNFTs map[ids.ID]NFTRequirement

	// Staking requirements per chain
	MinStakePerChain map[ids.ID]uint64
}

// NFTRequirement specifies NFT requirements for a specific chain
type NFTRequirement struct {
	ContractAddress string
	RequiredTokenIDs []uint64
	MinimumBalance  uint64 // Minimum number of NFTs required
}

// DefaultValidationConfig returns the default validation configuration
func DefaultValidationConfig() *ChainValidationConfig {
	return &ChainValidationConfig{
		// By default, validators run C, P, Q, X chains
		ValidateCChain: true,
		ValidatePChain: true,
		ValidateQChain: true,
		ValidateXChain: true,
		
		// Optional chains are opt-in
		ValidateAChain: false,
		ValidateBChain: false,
		ValidateMChain: false,
		ValidateZChain: false,

		MinStakePerChain: map[ids.ID]uint64{
			constants.PlatformChainID: 2000, // 2000 LUX minimum for P-Chain
			constants.CChainID:        1000, // 1000 LUX minimum for C-Chain
			constants.XChainID:        1000, // 1000 LUX minimum for X-Chain
			constants.QuantumChainID:  2000, // 2000 LUX minimum for Q-Chain
			constants.AIChainID:       5000, // 5000 LUX minimum for A-Chain
			constants.BridgeChainID:   3000, // 3000 LUX minimum for B-Chain
			constants.MPCChainID:      10000, // 10000 LUX minimum for M-Chain
			constants.ZKChainID:       5000, // 5000 LUX minimum for Z-Chain
		},
	}
}

// ValidatorChainManager manages validator opt-in for different chains
type ValidatorChainManager struct {
	// Validator configurations
	validatorConfigs map[ids.NodeID]*ChainValidationConfig
	
	// NFT verification interface
	nftVerifier NFTVerifier
	
	// Staking manager
	stakingManager StakingManager
	
	mu sync.RWMutex
}

// NFTVerifier interface for checking NFT ownership
type NFTVerifier interface {
	// VerifyNFTOwnership checks if a validator owns required NFTs
	VerifyNFTOwnership(validatorID ids.NodeID, contractAddress string, requiredTokenIDs []uint64) (bool, error)
}

// StakingManager interface for checking staking requirements
type StakingManager interface {
	// GetValidatorStake returns the stake amount for a validator on a specific chain
	GetValidatorStake(validatorID ids.NodeID, chainID ids.ID) (uint64, error)
}

// NewValidatorChainManager creates a new validator chain manager
func NewValidatorChainManager(nftVerifier NFTVerifier, stakingManager StakingManager) *ValidatorChainManager {
	return &ValidatorChainManager{
		validatorConfigs: make(map[ids.NodeID]*ChainValidationConfig),
		nftVerifier:      nftVerifier,
		stakingManager:   stakingManager,
	}
}

// RegisterValidator registers a validator with their chain preferences
func (vcm *ValidatorChainManager) RegisterValidator(validatorID ids.NodeID, config *ChainValidationConfig) error {
	vcm.mu.Lock()
	defer vcm.mu.Unlock()

	// Validate P-Chain and Q-Chain requirements
	if !config.ValidatePChain || !config.ValidateQChain {
		return errors.New("validators must participate in P-Chain and Q-Chain")
	}

	// Check Genesis NFT requirements for B-Chain and M-Chain
	if config.ValidateBChain || config.ValidateMChain {
		if config.GenesisNFTContract == "" || len(config.GenesisNFTTokenIDs) == 0 {
			return errors.New("B-Chain and M-Chain validation requires Genesis NFT ownership")
		}

		// Verify Genesis NFT ownership
		hasGenesisNFT, err := vcm.nftVerifier.VerifyNFTOwnership(validatorID, config.GenesisNFTContract, config.GenesisNFTTokenIDs)
		if err != nil {
			return err
		}
		if !hasGenesisNFT {
			return errors.New("validator does not own required Genesis NFTs for B/M-Chain validation")
		}
	}

	// Check chain-specific NFT requirements
	if config.ChainSpecificNFTs != nil {
		for chainID, nftReq := range config.ChainSpecificNFTs {
			// Only check if validator opted into this chain
			if !vcm.isChainOptedIn(config, chainID) {
				continue
			}

			hasNFT, err := vcm.nftVerifier.VerifyNFTOwnership(validatorID, nftReq.ContractAddress, nftReq.RequiredTokenIDs)
			if err != nil {
				return err
			}
			if !hasNFT {
				return errors.New("validator does not own required NFTs for chain " + chainID.String())
			}
		}
	}

	// Verify staking requirements for each opted-in chain
	if err := vcm.verifyStakingRequirements(validatorID, config); err != nil {
		return err
	}

	vcm.validatorConfigs[validatorID] = config
	return nil
}

// verifyStakingRequirements checks if validator meets staking requirements
func (vcm *ValidatorChainManager) verifyStakingRequirements(validatorID ids.NodeID, config *ChainValidationConfig) error {
	chainsToValidate := vcm.getOptedInChains(config)
	
	for _, chainID := range chainsToValidate {
		minStake, exists := config.MinStakePerChain[chainID]
		if !exists {
			continue // No minimum stake requirement
		}

		stake, err := vcm.stakingManager.GetValidatorStake(validatorID, chainID)
		if err != nil {
			return err
		}

		if stake < minStake {
			return errors.New("insufficient stake for chain " + chainID.String())
		}
	}

	return nil
}

// getOptedInChains returns the list of chains a validator has opted into
func (vcm *ValidatorChainManager) getOptedInChains(config *ChainValidationConfig) []ids.ID {
	var chains []ids.ID

	if config.ValidateAChain {
		chains = append(chains, constants.AIChainID)
	}
	if config.ValidateBChain {
		chains = append(chains, constants.BridgeChainID)
	}
	if config.ValidateCChain {
		chains = append(chains, constants.CChainID)
	}
	if config.ValidateMChain {
		chains = append(chains, constants.MPCChainID)
	}
	if config.ValidatePChain {
		chains = append(chains, constants.PlatformChainID)
	}
	if config.ValidateQChain {
		chains = append(chains, constants.QuantumChainID)
	}
	if config.ValidateXChain {
		chains = append(chains, constants.XChainID)
	}
	if config.ValidateZChain {
		chains = append(chains, constants.ZKChainID)
	}

	return chains
}

// CanValidateChain checks if a validator can validate a specific chain
func (vcm *ValidatorChainManager) CanValidateChain(validatorID ids.NodeID, chainID ids.ID) bool {
	vcm.mu.RLock()
	defer vcm.mu.RUnlock()

	config, exists := vcm.validatorConfigs[validatorID]
	if !exists {
		return false
	}

	switch chainID {
	case constants.AIChainID:
		return config.ValidateAChain
	case constants.BridgeChainID:
		return config.ValidateBChain
	case constants.CChainID:
		return config.ValidateCChain
	case constants.MPCChainID:
		return config.ValidateMChain
	case constants.PlatformChainID:
		return config.ValidatePChain
	case constants.QuantumChainID:
		return config.ValidateQChain
	case constants.XChainID:
		return config.ValidateXChain
	case constants.ZKChainID:
		return config.ValidateZChain
	default:
		return false
	}
}

// GetValidatorsForChain returns all validators that can validate a specific chain
func (vcm *ValidatorChainManager) GetValidatorsForChain(chainID ids.ID) []ids.NodeID {
	vcm.mu.RLock()
	defer vcm.mu.RUnlock()

	var validators []ids.NodeID
	for validatorID, config := range vcm.validatorConfigs {
		if vcm.canValidateChainWithConfig(config, chainID) {
			validators = append(validators, validatorID)
		}
	}
	return validators
}

// canValidateChainWithConfig checks if a config allows validating a chain
func (vcm *ValidatorChainManager) canValidateChainWithConfig(config *ChainValidationConfig, chainID ids.ID) bool {
	switch chainID {
	case constants.AIChainID:
		return config.ValidateAChain
	case constants.BridgeChainID:
		return config.ValidateBChain
	case constants.CChainID:
		return config.ValidateCChain
	case constants.MPCChainID:
		return config.ValidateMChain
	case constants.PlatformChainID:
		return config.ValidatePChain
	case constants.QuantumChainID:
		return config.ValidateQChain
	case constants.XChainID:
		return config.ValidateXChain
	case constants.ZKChainID:
		return config.ValidateZChain
	default:
		return false
	}
}

// GetValidatorConfig returns the configuration for a specific validator
func (vcm *ValidatorChainManager) GetValidatorConfig(validatorID ids.NodeID) (*ChainValidationConfig, bool) {
	vcm.mu.RLock()
	defer vcm.mu.RUnlock()

	config, exists := vcm.validatorConfigs[validatorID]
	return config, exists
}

// UpdateValidatorConfig updates a validator's chain preferences
func (vcm *ValidatorChainManager) UpdateValidatorConfig(validatorID ids.NodeID, config *ChainValidationConfig) error {
	// Re-validate and update
	return vcm.RegisterValidator(validatorID, config)
}

// isChainOptedIn checks if a validator has opted into a specific chain
func (vcm *ValidatorChainManager) isChainOptedIn(config *ChainValidationConfig, chainID ids.ID) bool {
	switch chainID {
	case constants.AIChainID:
		return config.ValidateAChain
	case constants.BridgeChainID:
		return config.ValidateBChain
	case constants.CChainID:
		return config.ValidateCChain
	case constants.MPCChainID:
		return config.ValidateMChain
	case constants.PlatformChainID:
		return config.ValidatePChain
	case constants.QuantumChainID:
		return config.ValidateQChain
	case constants.XChainID:
		return config.ValidateXChain
	case constants.ZKChainID:
		return config.ValidateZChain
	default:
		return false
	}
}

// GetChainValidatorSet returns the validator set for a specific chain
func (vcm *ValidatorChainManager) GetChainValidatorSet(chainID ids.ID) (*ChainValidatorSet, error) {
	vcm.mu.RLock()
	defer vcm.mu.RUnlock()

	validators := vcm.GetValidatorsForChain(chainID)
	if len(validators) == 0 {
		return nil, errors.New("no validators for chain " + chainID.String())
	}

	// Check if this is a Genesis NFT-gated chain
	isGenesisGated := chainID == constants.BridgeChainID || chainID == constants.MPCChainID

	return &ChainValidatorSet{
		ChainID:        chainID,
		Validators:     validators,
		IsGenesisGated: isGenesisGated,
		MinValidators:  vcm.getMinValidatorsForChain(chainID),
	}, nil
}

// getMinValidatorsForChain returns the minimum number of validators required for a chain
func (vcm *ValidatorChainManager) getMinValidatorsForChain(chainID ids.ID) int {
	switch chainID {
	case constants.PlatformChainID, constants.QuantumChainID:
		return 21 // Core chains need full validator set
	case constants.CChainID, constants.XChainID:
		return 15 // Important user-facing chains
	case constants.BridgeChainID, constants.MPCChainID:
		return 7 // Genesis NFT-gated chains can have fewer validators
	case constants.AIChainID, constants.ZKChainID:
		return 5 // Specialized opt-in chains
	default:
		return 3 // Minimum viable validator set
	}
}

// ChainValidatorSet represents the validator set for a specific chain
type ChainValidatorSet struct {
	ChainID        ids.ID
	Validators     []ids.NodeID
	IsGenesisGated bool
	MinValidators  int
}
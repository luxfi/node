// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/luxfi/node/ids"
)

// NFTGenesisConfig defines the initial NFT distribution
type NFTGenesisConfig struct {
	// Genesis NFT collection on Ethereum (lux.town)
	EthereumContract   common.Address `json:"ethereumContract"`
	EthereumDeployment uint64         `json:"ethereumDeployment"`
	
	// Lux Network NFT collections
	Collections []NFTCollection `json:"collections"`
	
	// Initial validator NFTs
	ValidatorNFTs []ValidatorNFT `json:"validatorNFTs"`
	
	// NFT-based mining configuration
	MiningConfig NFTMiningConfig `json:"miningConfig"`
}

// NFTCollection represents an NFT collection on Lux
type NFTCollection struct {
	Name            string              `json:"name"`
	Symbol          string              `json:"symbol"`
	ContractAddress string              `json:"contractAddress"`
	ChainID         string              `json:"chainId"` // X, C, or P
	TotalSupply     uint64              `json:"totalSupply"`
	Metadata        CollectionMetadata  `json:"metadata"`
	Traits          []NFTTrait          `json:"traits"`
}

// ValidatorNFT represents a validator NFT
type ValidatorNFT struct {
	TokenID          uint64         `json:"tokenId"`
	OriginalOwner    common.Address `json:"originalOwner"`    // ETH address from lux.town
	LuxOwner         ids.ShortID    `json:"luxOwner"`         // Lux address
	Tier             string         `json:"tier"`             // Genesis, Pioneer, Standard
	StakingPower     uint64         `json:"stakingPower"`     // Effective staking power
	MiningMultiplier uint32         `json:"miningMultiplier"` // Mining reward multiplier
	Attributes       NFTAttributes  `json:"attributes"`
}

// NFTAttributes contains NFT metadata
type NFTAttributes struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Image       string            `json:"image"`
	Properties  map[string]string `json:"properties"`
}

// NFTMiningConfig defines NFT-based mining parameters
type NFTMiningConfig struct {
	Enabled              bool                    `json:"enabled"`
	RequiresNFT          bool                    `json:"requiresNFT"`
	BaseReward           uint64                  `json:"baseReward"`
	NFTMultipliers       map[string]uint32       `json:"nftMultipliers"` // tier -> multiplier
	MiningPools          []MiningPool            `json:"miningPools"`
}

// MiningPool represents a mining pool configuration
type MiningPool struct {
	PoolID          string   `json:"poolId"`
	ChainID         string   `json:"chainId"`
	RequiredNFTTier string   `json:"requiredNftTier"`
	RewardPerBlock  uint64   `json:"rewardPerBlock"`
	Active          bool     `json:"active"`
}

// CollectionMetadata contains collection-level metadata
type CollectionMetadata struct {
	Description  string `json:"description"`
	ExternalURL  string `json:"externalUrl"`
	ImageURL     string `json:"imageUrl"`
	AnimationURL string `json:"animationUrl,omitempty"`
}

// NFTTrait represents possible traits for NFTs
type NFTTrait struct {
	TraitType string   `json:"traitType"`
	Values    []string `json:"values"`
	Rarity    []uint32 `json:"rarity"` // Percentage for each value
}

// DefaultNFTGenesis returns the default NFT genesis configuration
func DefaultNFTGenesis() *NFTGenesisConfig {
	return &NFTGenesisConfig{
		// Original lux.town contract on Ethereum
		EthereumContract:   common.HexToAddress("0x0000000000000000000000000000000000000000"), // Replace with actual
		EthereumDeployment: 17000000, // Replace with actual block
		
		Collections: []NFTCollection{
			{
				Name:            "Lux Genesis Validators",
				Symbol:          "LUXGEN",
				ContractAddress: "0x0100000000000000000000000000000000000001",
				ChainID:         "C", // Deploy on C-Chain
				TotalSupply:     100, // Initial 100 genesis NFTs
				Metadata: CollectionMetadata{
					Description: "Genesis validator NFTs for the Lux Network",
					ExternalURL: "https://lux.network/validators",
					ImageURL:    "https://lux.network/nft/genesis/",
				},
				Traits: []NFTTrait{
					{
						TraitType: "Tier",
						Values:    []string{"Genesis", "Pioneer", "Standard"},
						Rarity:    []uint32{10, 30, 60}, // 10% Genesis, 30% Pioneer, 60% Standard
					},
					{
						TraitType: "Power",
						Values:    []string{"High", "Medium", "Low"},
						Rarity:    []uint32{20, 50, 30},
					},
				},
			},
		},
		
		ValidatorNFTs: generateGenesisValidatorNFTs(),
		
		MiningConfig: NFTMiningConfig{
			Enabled:     true,
			RequiresNFT: true, // Must own NFT to mine
			BaseReward:  100 * 1e9, // 100 LUX base reward
			NFTMultipliers: map[string]uint32{
				"Genesis":  300, // 3x mining rewards
				"Pioneer":  200, // 2x mining rewards
				"Standard": 100, // 1x mining rewards
			},
			MiningPools: []MiningPool{
				{
					PoolID:          "genesis-pool",
					ChainID:         "X",
					RequiredNFTTier: "Genesis",
					RewardPerBlock:  1000 * 1e9, // 1000 LUX per block
					Active:          true,
				},
				{
					PoolID:          "pioneer-pool",
					ChainID:         "X",
					RequiredNFTTier: "Pioneer",
					RewardPerBlock:  500 * 1e9, // 500 LUX per block
					Active:          true,
				},
				{
					PoolID:          "standard-pool",
					ChainID:         "X",
					RequiredNFTTier: "Standard",
					RewardPerBlock:  100 * 1e9, // 100 LUX per block
					Active:          true,
				},
			},
		},
	}
}

// generateGenesisValidatorNFTs generates the initial 100 validator NFTs
func generateGenesisValidatorNFTs() []ValidatorNFT {
	nfts := make([]ValidatorNFT, 100)
	
	// First 10 are Genesis tier
	for i := 0; i < 10; i++ {
		nfts[i] = ValidatorNFT{
			TokenID:       uint64(i + 1),
			OriginalOwner: common.HexToAddress("0x0"), // Will be mapped from lux.town
			LuxOwner:      ids.ShortEmpty,             // To be claimed
			Tier:          "Genesis",
			StakingPower:  1000000 * 1e9, // 1M LUX equivalent
			MiningMultiplier: 300,
			Attributes: NFTAttributes{
				Name:        "Lux Genesis Validator #" + string(rune(i+1)),
				Description: "Genesis tier validator NFT with maximum rewards",
				Image:       "ipfs://QmGenesis" + string(rune(i+1)),
				Properties: map[string]string{
					"tier":       "Genesis",
					"power":      "High",
					"rarity":     "Legendary",
					"genesis":    "true",
				},
			},
		}
	}
	
	// Next 30 are Pioneer tier
	for i := 10; i < 40; i++ {
		nfts[i] = ValidatorNFT{
			TokenID:       uint64(i + 1),
			OriginalOwner: common.HexToAddress("0x0"),
			LuxOwner:      ids.ShortEmpty,
			Tier:          "Pioneer",
			StakingPower:  750000 * 1e9, // 750K LUX equivalent
			MiningMultiplier: 200,
			Attributes: NFTAttributes{
				Name:        "Lux Pioneer Validator #" + string(rune(i+1)),
				Description: "Pioneer tier validator NFT with enhanced rewards",
				Image:       "ipfs://QmPioneer" + string(rune(i+1)),
				Properties: map[string]string{
					"tier":    "Pioneer",
					"power":   "Medium",
					"rarity":  "Rare",
					"genesis": "false",
				},
			},
		}
	}
	
	// Remaining 60 are Standard tier
	for i := 40; i < 100; i++ {
		nfts[i] = ValidatorNFT{
			TokenID:       uint64(i + 1),
			OriginalOwner: common.HexToAddress("0x0"),
			LuxOwner:      ids.ShortEmpty,
			Tier:          "Standard",
			StakingPower:  500000 * 1e9, // 500K LUX equivalent
			MiningMultiplier: 100,
			Attributes: NFTAttributes{
				Name:        "Lux Standard Validator #" + string(rune(i+1)),
				Description: "Standard tier validator NFT",
				Image:       "ipfs://QmStandard" + string(rune(i+1)),
				Properties: map[string]string{
					"tier":    "Standard",
					"power":   "Low",
					"rarity":  "Common",
					"genesis": "false",
				},
			},
		}
	}
	
	return nfts
}

// GetNFTGenesisState returns the serialized NFT genesis state
func GetNFTGenesisState() ([]byte, error) {
	config := DefaultNFTGenesis()
	return json.MarshalIndent(config, "", "  ")
}

// NFTCrossChainSupport defines cross-chain NFT functionality
type NFTCrossChainSupport struct {
	// X-Chain: NFT trading and marketplace
	XChainEnabled bool `json:"xChainEnabled"`
	XChainMarket  bool `json:"xChainMarket"`
	
	// C-Chain: NFT smart contracts and DeFi integration
	CChainEnabled bool   `json:"cChainEnabled"`
	CChainContract string `json:"cChainContract"`
	
	// P-Chain: NFT staking and delegation
	PChainEnabled  bool `json:"pChainEnabled"`
	PChainStaking  bool `json:"pChainStaking"`
	PChainDelegate bool `json:"pChainDelegate"`
}

// DefaultCrossChainSupport returns default cross-chain configuration
func DefaultCrossChainSupport() *NFTCrossChainSupport {
	return &NFTCrossChainSupport{
		XChainEnabled:  true,
		XChainMarket:   true,
		CChainEnabled:  true,
		CChainContract: "0x0100000000000000000000000000000000000001",
		PChainEnabled:  true,
		PChainStaking:  true,
		PChainDelegate: true,
	}
}

// ValidateNFTOwnership validates NFT ownership across chains
func ValidateNFTOwnership(chainID string, owner ids.ShortID, tokenID uint64) (bool, error) {
	// Implementation would check ownership on the specified chain
	// This is a placeholder for the actual implementation
	return true, nil
}

// GetNFTStakingPower returns the effective staking power of an NFT
func GetNFTStakingPower(tokenID uint64) (uint64, error) {
	nfts := generateGenesisValidatorNFTs()
	for _, nft := range nfts {
		if nft.TokenID == tokenID {
			return nft.StakingPower, nil
		}
	}
	return 0, errors.New("NFT not found")
}
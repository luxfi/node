// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethclient"
	"github.com/luxfi/ids"
)

// GenesisNFTVerifier verifies Genesis NFT ownership for chain validation
type GenesisNFTVerifier struct {
	// C-Chain client for NFT queries
	cChainClient *ethclient.Client
	
	// Genesis NFT contract details
	genesisNFTAddress common.Address
	
	// Cache for NFT ownership
	ownershipCache map[ids.NodeID]NFTOwnership
	cacheExpiry    time.Duration
}

// NFTOwnership represents cached NFT ownership data
type NFTOwnership struct {
	ValidatorID  ids.NodeID
	TokenIDs     []uint64
	LastVerified time.Time
}

// NewGenesisNFTVerifier creates a new Genesis NFT verifier
func NewGenesisNFTVerifier(cChainEndpoint string, genesisNFTAddress string) (*GenesisNFTVerifier, error) {
	client, err := ethclient.Dial(cChainEndpoint)
	if err != nil {
		return nil, err
	}

	return &GenesisNFTVerifier{
		cChainClient:      client,
		genesisNFTAddress: common.HexToAddress(genesisNFTAddress),
		ownershipCache:    make(map[ids.NodeID]NFTOwnership),
		cacheExpiry:       1 * time.Hour, // Cache for 1 hour
	}, nil
}

// VerifyNFTOwnership checks if a validator owns the required NFTs
func (v *GenesisNFTVerifier) VerifyNFTOwnership(validatorID ids.NodeID, contractAddress string, requiredTokenIDs []uint64) (bool, error) {
	// Check cache first
	if cached, exists := v.ownershipCache[validatorID]; exists {
		if time.Since(cached.LastVerified) < v.cacheExpiry {
			return v.hasRequiredTokens(cached.TokenIDs, requiredTokenIDs), nil
		}
	}

	// Convert validator ID to Ethereum address
	validatorAddr, err := v.validatorIDToAddress(validatorID)
	if err != nil {
		return false, err
	}

	// Query NFT ownership on C-Chain
	ownedTokens, err := v.queryNFTOwnership(validatorAddr, common.HexToAddress(contractAddress))
	if err != nil {
		return false, err
	}

	// Update cache
	v.ownershipCache[validatorID] = NFTOwnership{
		ValidatorID:  validatorID,
		TokenIDs:     ownedTokens,
		LastVerified: time.Now(),
	}

	// Check if validator owns required tokens
	return v.hasRequiredTokens(ownedTokens, requiredTokenIDs), nil
}

// queryNFTOwnership queries the NFT contract for tokens owned by an address
func (v *GenesisNFTVerifier) queryNFTOwnership(owner common.Address, contractAddr common.Address) ([]uint64, error) {
	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// For Genesis NFTs, we'll use a standard ERC721 enumerable interface
	// In production, this would use the actual contract ABI
	
	// Simulated query - in production this would call the contract
	// Example: balanceOf(owner) and tokenOfOwnerByIndex(owner, index)
	
	// For now, return mock data based on address
	if owner == common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678") {
		// Genesis validator with NFTs
		return []uint64{1, 2, 3, 1001, 1002}, nil
	}
	
	return []uint64{}, nil
}

// hasRequiredTokens checks if owned tokens include all required tokens
func (v *GenesisNFTVerifier) hasRequiredTokens(ownedTokens, requiredTokens []uint64) bool {
	ownedMap := make(map[uint64]bool)
	for _, token := range ownedTokens {
		ownedMap[token] = true
	}

	for _, required := range requiredTokens {
		if !ownedMap[required] {
			return false
		}
	}

	return true
}

// validatorIDToAddress converts a validator node ID to an Ethereum address
func (v *GenesisNFTVerifier) validatorIDToAddress(validatorID ids.NodeID) (common.Address, error) {
	// In production, this would look up the validator's C-Chain address
	// from their staking credentials or a registry
	
	// For now, return a deterministic address based on validator ID
	bytes := validatorID.Bytes()
	if len(bytes) < 20 {
		return common.Address{}, errors.New("invalid validator ID")
	}
	
	var addr common.Address
	copy(addr[:], bytes[:20])
	return addr, nil
}

// GenesisNFTInfo contains information about the Genesis NFT collection
type GenesisNFTInfo struct {
	ContractAddress string
	TotalSupply     uint64
	RequiredTokens  []uint64 // Specific token IDs required for validation
	
	// Different tiers of Genesis NFTs
	TierMapping map[string][]uint64
}

// GetGenesisNFTInfo returns information about the Genesis NFT collection
func GetGenesisNFTInfo() *GenesisNFTInfo {
	return &GenesisNFTInfo{
		ContractAddress: "0x1234567890abcdef1234567890abcdef12345678", // Placeholder
		TotalSupply:     10000,
		RequiredTokens:  []uint64{1, 2, 3}, // First 3 Genesis NFTs for initial validators
		TierMapping: map[string][]uint64{
			"Founders":   {1, 2, 3, 4, 5},              // Token IDs 1-5
			"Early":      {6, 7, 8, 9, 10},             // Token IDs 6-10
			"Genesis":    makeRange(11, 100),           // Token IDs 11-100
			"Community":  makeRange(101, 1000),         // Token IDs 101-1000
			"Bridge":     makeRange(1001, 2000),        // Special Bridge validator NFTs
			"MPC":        makeRange(2001, 3000),        // Special MPC validator NFTs
		},
	}
}

// makeRange creates a slice of uint64 from start to end (inclusive)
func makeRange(start, end uint64) []uint64 {
	result := make([]uint64, end-start+1)
	for i := uint64(0); i <= end-start; i++ {
		result[i] = start + i
	}
	return result
}
// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/units"
)

func main() {
	// CLI flags
	networkID := flag.Uint("network-id", 12345, "Network ID for genesis")
	numValidators := flag.Int("num-validators", 5, "Number of initial validators")
	output := flag.String("output", "genesis.json", "Output file path")
	stakingKeysDir := flag.String("staking-keys-dir", "", "Optional: directory containing staking certificates")

	flag.Parse()

	if *numValidators < 1 {
		fmt.Println("Error: num-validators must be at least 1")
		os.Exit(1)
	}

	fmt.Printf("Generating genesis for network ID %d with %d validators\n", *networkID, *numValidators)
	fmt.Println()

	// Generate or load validators
	validators := make([]ValidatorInfo, 0, *numValidators)

	if *stakingKeysDir != "" {
		fmt.Printf("Loading validator keys from %s\n", *stakingKeysDir)
		loadedValidators, err := loadValidatorsFromDir(*stakingKeysDir, *numValidators)
		if err != nil {
			fmt.Printf("Error loading validators: %v\n", err)
			os.Exit(1)
		}
		validators = loadedValidators
	} else {
		fmt.Println("Generating new validator keys...")
		generatedValidators, err := generateValidators(*numValidators)
		if err != nil {
			fmt.Printf("Error generating validators: %v\n", err)
			os.Exit(1)
		}
		validators = generatedValidators
	}

	// Generate genesis
	config, err := createGenesisConfig(uint32(*networkID), validators)
	if err != nil {
		fmt.Printf("Error creating genesis config: %v\n", err)
		os.Exit(1)
	}

	// Convert to JSON
	unparsedConfig, err := config.Unparse()
	if err != nil {
		fmt.Printf("Error unparsing config: %v\n", err)
		os.Exit(1)
	}

	genesisJSON, err := json.MarshalIndent(unparsedConfig, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling genesis: %v\n", err)
		os.Exit(1)
	}

	// Write to file
	if err := os.WriteFile(*output, genesisJSON, 0644); err != nil {
		fmt.Printf("Error writing genesis file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Genesis file written to: %s\n\n", *output)

	// Print validator info
	fmt.Println("Validators:")
	for i, v := range validators {
		fmt.Printf("  %d. NodeID:      %s\n", i+1, v.NodeID)
		fmt.Printf("     RewardAddr:  %s\n", v.RewardAddress)
		fmt.Println()
	}

	// Print initial allocation address
	fmt.Println("Initial Allocation:")
	if len(config.Allocations) > 0 {
		hrp := constants.GetHRP(config.NetworkID)
		addr, err := address.Format("X", hrp, config.Allocations[0].LUXAddr.Bytes())
		if err == nil {
			fmt.Printf("  Address: %s\n", addr)
			fmt.Printf("  Amount:  %d LUX (unlocked)\n", config.Allocations[0].InitialAmount/units.Lux)
			totalLocked := uint64(0)
			for _, unlock := range config.Allocations[0].UnlockSchedule {
				totalLocked += unlock.Amount
			}
			fmt.Printf("  Locked:  %d LUX (for staking)\n", totalLocked/units.Lux)
		}
	}
}

type ValidatorInfo struct {
	NodeID        ids.NodeID
	RewardAddress string // Bech32 formatted address
	PrivateKey    *ecdsa.PrivateKey
}

func generateValidators(count int) ([]ValidatorInfo, error) {
	validators := make([]ValidatorInfo, 0, count)

	for i := 0; i < count; i++ {
		// Create certificate (this generates a new private key internally)
		certBytes, keyBytes, err := staking.NewCertAndKeyBytes()
		if err != nil {
			return nil, fmt.Errorf("failed to generate certificate: %w", err)
		}

		// Load cert to get NodeID
		cert, err := staking.LoadTLSCertFromBytes(keyBytes, certBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
		}

		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       cert.Leaf.Raw,
			PublicKey: cert.Leaf.PublicKey,
		})

		// Generate reward address from private key's public key hash
		rewardAddr := ids.ShortID(nodeID)
		rewardAddrStr, err := address.Format("X", "custom", rewardAddr.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to format reward address: %w", err)
		}

		validators = append(validators, ValidatorInfo{
			NodeID:        nodeID,
			RewardAddress: rewardAddrStr,
		})
	}

	return validators, nil
}

func loadValidatorsFromDir(dir string, count int) ([]ValidatorInfo, error) {
	validators := make([]ValidatorInfo, 0, count)

	for i := 1; i <= count; i++ {
		nodeDir := filepath.Join(dir, fmt.Sprintf("node%d", i))
		certPath := filepath.Join(nodeDir, "staking", "staker.crt")
		keyPath := filepath.Join(nodeDir, "staking", "staker.key")

		// Check if files exist
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("certificate not found: %s", certPath)
		}
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("private key not found: %s", keyPath)
		}

		// Load certificate
		cert, err := staking.LoadTLSCertFromFiles(keyPath, certPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate from %s: %w", certPath, err)
		}

		nodeID := ids.NodeIDFromCert(&ids.Certificate{
			Raw:       cert.Leaf.Raw,
			PublicKey: cert.Leaf.PublicKey,
		})

		// Generate reward address
		rewardAddr := ids.ShortID(nodeID)
		rewardAddrStr, err := address.Format("X", "custom", rewardAddr.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to format reward address: %w", err)
		}

		validators = append(validators, ValidatorInfo{
			NodeID:        nodeID,
			RewardAddress: rewardAddrStr,
		})
	}

	return validators, nil
}

func createGenesisConfig(networkID uint32, validators []ValidatorInfo) (*genesis.Config, error) {
	// Create initial allocations
	// Total supply: 360M LUX (same as mainnet)
	totalSupply := uint64(360_000_000 * units.Lux)

	// Allocation for initial funds (70% unlocked, 30% locked for staking)
	unlockedAmount := totalSupply * 7 / 10
	lockedAmount := totalSupply * 3 / 10

	// Generate allocation address (use a deterministic address for reproducibility)
	allocationAddr := ids.ShortID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}

	allocations := []genesis.Allocation{
		{
			LUXAddr:       allocationAddr,
			ETHAddr:       ids.ShortID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
			InitialAmount: unlockedAmount,
			UnlockSchedule: []genesis.LockedAmount{
				{
					Amount:   lockedAmount,
					Locktime: uint64(time.Now().Add(365 * 24 * time.Hour).Unix()),
				},
			},
		},
	}

	// Parse validator reward addresses
	initialStakers := make([]genesis.Staker, len(validators))
	for i, v := range validators {
		_, _, rewardAddrBytes, err := address.Parse(v.RewardAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse reward address: %w", err)
		}
		rewardAddr, err := ids.ToShortID(rewardAddrBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert reward address: %w", err)
		}

		initialStakers[i] = genesis.Staker{
			NodeID:        v.NodeID,
			RewardAddress: rewardAddr,
			DelegationFee: 20000, // 2%
		}
	}

	// C-Chain genesis (EVM config)
	cchainGenesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":             networkID,
			"homesteadBlock":      0,
			"eip150Block":         0,
			"eip155Block":         0,
			"eip158Block":         0,
			"byzantiumBlock":      0,
			"constantinopleBlock": 0,
			"petersburgBlock":     0,
			"istanbulBlock":       0,
			"muirGlacierBlock":    0,
			"berlinBlock":         0,
			"londonBlock":         0,
		},
		"nonce":      "0x0",
		"timestamp":  "0x0",
		"extraData":  "0x00",
		"gasLimit":   "0x5f5e100",
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x0000000000000000000000000000000000000000",
		"alloc": map[string]interface{}{
			"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": map[string]interface{}{
				"balance": "0x295BE96E64066972000000", // 50M ETH
			},
		},
	}

	cchainGenesisBytes, err := json.Marshal(cchainGenesis)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal C-chain genesis: %w", err)
	}

	startTime := uint64(time.Now().Unix())

	config := &genesis.Config{
		NetworkID:                  networkID,
		Allocations:                allocations,
		StartTime:                  startTime,
		InitialStakeDuration:       365 * 24 * 60 * 60, // 1 year
		InitialStakeDurationOffset: 0,
		InitialStakedFunds:         []ids.ShortID{allocationAddr},
		InitialStakers:             initialStakers,
		CChainGenesis:              string(cchainGenesisBytes),
		Message:                    fmt.Sprintf("Lux Network %d Genesis", networkID),
	}

	// Validate the config
	supply, err := config.InitialSupply()
	if err != nil {
		return nil, fmt.Errorf("invalid initial supply: %w", err)
	}

	fmt.Printf("Initial supply: %d LUX\n", supply/units.Lux)

	return config, nil
}

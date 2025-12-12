// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

// Const variables to be exported
const (
	// Standard network IDs (for compatibility)
	LocalID    uint32 = 1337
	MainnetID  uint32 = 1
	TestnetID  uint32 = 5
	UnitTestID uint32 = 369

	// Lux Network IDs
	// These are the PRIMARY NETWORK identifiers for the entire Lux blockchain
	// All chains (P, X, C, Q, etc.) run on these networks
	LuxMainnetID uint32 = 96369 // Lux mainnet - production network
	LuxTestnetID uint32 = 96368 // Lux testnet - test network (C-Chain EVM chain ID)

	// Q-Chain Network IDs (Quantum-resistant chain)
	QChainMainnetID uint32 = 36963 // Q-Chain mainnet
	QChainTestnetID uint32 = 36962 // Q-Chain testnet

	// Network name strings
	LocalName    = "local"
	MainnetName  = "mainnet"
	TestnetName  = "testnet"
	UnitTestName = "testing"

	// HRP (Human Readable Part) for bech32 addresses
	// Used to format P-chain and X-chain addresses like P-lux1..., X-test1...
	FallbackHRP = "custom" // Used for devnets/custom networks
	LocalHRP    = "local"  // local1... for local development
	MainnetHRP  = "lux"    // lux1... for mainnet
	TestnetHRP  = "test"   // test1... for testnet
	UnitTestHRP = "testing"
)

// Variables to be exported
var (
	PrimaryNetworkID = ids.Empty
	PlatformChainID  = ids.PChainID // P-Chain: 11111111111111111111111111111111P

	// Chain IDs - these identify specific chains WITHIN a network
	// NOT to be confused with Network IDs
	// Native chains have a recognizable pattern: all zeros except last byte which is the chain letter
	// These are provided by the ids package for consistent display across the ecosystem
	CChainID = ids.CChainID // C-Chain: 11111111111111111111111111111111C
	XChainID = ids.XChainID // X-Chain: 11111111111111111111111111111111X
	QChainID = ids.QChainID // Q-Chain: 11111111111111111111111111111111Q
	AChainID = ids.AChainID // A-Chain: 11111111111111111111111111111111A
	BChainID = ids.BChainID // B-Chain: 11111111111111111111111111111111B
	TChainID = ids.TChainID // T-Chain: 11111111111111111111111111111111T
	ZChainID = ids.ZChainID // Z-Chain: 11111111111111111111111111111111Z (Zero-knowledge)
	GChainID = ids.GChainID // G-Chain: 11111111111111111111111111111111G (Graph/dgraph) - COMING SOON
	IChainID = ids.IChainID // I-Chain: 11111111111111111111111111111111I (Identity) - COMING SOON
	KChainID = ids.KChainID // K-Chain: 11111111111111111111111111111111K (KMS) - COMING SOON

	// NetworkIDToNetworkName maps network IDs to human-readable names
	NetworkIDToNetworkName = map[uint32]string{
		LocalID:      LocalName,
		MainnetID:    MainnetName,
		TestnetID:    TestnetName,
		UnitTestID:   UnitTestName,
		LuxMainnetID: MainnetName, // 96369 -> "mainnet"
		LuxTestnetID: TestnetName, // 96368 -> "testnet"
	}

	// NetworkNameToNetworkID maps names to network IDs
	NetworkNameToNetworkID = map[string]uint32{
		LocalName:    LocalID,
		MainnetName:  MainnetID,
		TestnetName:  TestnetID,
		UnitTestName: UnitTestID,
	}

	// NetworkIDToHRP maps network IDs to bech32 HRP (Human Readable Part)
	// This determines the address prefix: P-lux1..., P-test1..., P-local1..., P-custom1...
	NetworkIDToHRP = map[uint32]string{
		LocalID:      LocalHRP,    // local1...
		MainnetID:    MainnetHRP,  // lux1...
		TestnetID:    TestnetHRP,  // test1...
		UnitTestID:   UnitTestHRP, // testing1...
		LuxMainnetID: MainnetHRP,  // 96369 -> lux1...
		LuxTestnetID: TestnetHRP,  // 96368 -> test1...
	}

	// NetworkHRPToNetworkID maps HRP back to network ID
	NetworkHRPToNetworkID = map[string]uint32{
		LocalHRP:    LocalID,
		MainnetHRP:  MainnetID,
		TestnetHRP:  TestnetID,
		UnitTestHRP: UnitTestID,
	}

	// ProductionNetworkIDs are networks that should use production-grade settings
	ProductionNetworkIDs = set.Of(MainnetID, TestnetID, LuxMainnetID, LuxTestnetID)

	ValidNetworkPrefix = "network-"

	ErrParseNetworkName = errors.New("failed to parse network name")
)

// GetHRP returns the Human-Readable-Part of bech32 addresses for a networkID
func GetHRP(networkID uint32) string {
	if hrp, ok := NetworkIDToHRP[networkID]; ok {
		return hrp
	}
	return FallbackHRP
}

// NetworkName returns a human readable name for the network with
// ID [networkID]
func NetworkName(networkID uint32) string {
	if name, exists := NetworkIDToNetworkName[networkID]; exists {
		return name
	}
	return fmt.Sprintf("network-%d", networkID)
}

// NetworkID returns the ID of the network with name [networkName]
func NetworkID(networkName string) (uint32, error) {
	networkName = strings.ToLower(networkName)
	if id, exists := NetworkNameToNetworkID[networkName]; exists {
		return id, nil
	}

	idStr := networkName
	if strings.HasPrefix(networkName, ValidNetworkPrefix) {
		idStr = networkName[len(ValidNetworkPrefix):]
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrParseNetworkName, networkName)
	}
	return uint32(id), nil
}

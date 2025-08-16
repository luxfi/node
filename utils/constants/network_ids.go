// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// Const variables to be exported
const (
	LocalID    uint32 = 31337
	MainnetID  uint32 = 1 // Use 1 for Lux compatibility
	TestnetID  uint32 = 5 // Use 5 for Lux compatibility
	UnitTestID uint32 = 369

	// Lux-specific network IDs
	LuxMainnetID uint32 = 96369
	LuxTestnetID uint32 = 96368

	LocalName    = "local"
	MainnetName  = "mainnet"
	TestnetName  = "testnet"
	UnitTestName = "testing"

	FallbackHRP = "custom"
	LocalHRP    = "local"
	MainnetHRP  = "lux"
	TestnetHRP  = "test"
	UnitTestHRP = "testing"
)

// Variables to be exported
var (
	PrimaryNetworkID = ids.Empty
	PlatformChainID  = ids.Empty

	NetworkIDToNetworkName = map[uint32]string{
		LocalID:      LocalName,
		MainnetID:    MainnetName,
		TestnetID:    TestnetName,
		UnitTestID:   UnitTestName,
		LuxMainnetID: MainnetName, // Also map Lux IDs to the same names
		LuxTestnetID: TestnetName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		LocalName:    LocalID,
		MainnetName:  MainnetID,
		TestnetName:  TestnetID,
		UnitTestName: UnitTestID,
	}

	NetworkIDToHRP = map[uint32]string{
		LocalID:      LocalHRP,
		MainnetID:    MainnetHRP,
		TestnetID:    TestnetHRP,
		UnitTestID:   UnitTestHRP,
		LuxMainnetID: MainnetHRP, // Lux uses same HRP
		LuxTestnetID: TestnetHRP,
	}
	NetworkHRPToNetworkID = map[string]uint32{
		LocalHRP:    LocalID,
		MainnetHRP:  MainnetID,
		TestnetHRP:  TestnetID,
		UnitTestHRP: UnitTestID,
	}
	ProductionNetworkIDs = set.Of(MainnetID, TestnetID)

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

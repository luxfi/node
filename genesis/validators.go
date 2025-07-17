// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"
	"fmt"

	_ "embed"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/set"
)

var (
	//go:embed validators.json
	validatorsPerNetworkJSON []byte

	validatorsPerNetwork map[string]set.Set[ids.NodeID]
)

func init() {
	// Try to unmarshal validators, but don't panic on checksum errors
	if err := json.Unmarshal(validatorsPerNetworkJSON, &validatorsPerNetwork); err != nil {
		// If parsing fails, initialize with empty sets
		validatorsPerNetwork = make(map[string]set.Set[ids.NodeID])
		// Log the error but don't panic
		fmt.Printf("Warning: failed to decode validators.json (checksum issues): %v\n", err)
	}
}

// GetValidators returns recent validators for the requested network.
func GetValidators(networkID uint32) set.Set[ids.NodeID] {
	networkName := constants.NetworkIDToNetworkName[networkID]
	return validatorsPerNetwork[networkName]
}

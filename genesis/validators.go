// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
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
	// TODO: Fix validators.json checksum issues
	// if err := json.Unmarshal(validatorsPerNetworkJSON, &validatorsPerNetwork); err != nil {
	// 	panic(fmt.Sprintf("failed to decode validators.json: %v", err))
	// }
	validatorsPerNetwork = make(map[string]set.Set[ids.NodeID])
}

// GetValidators returns recent validators for the requested network.
func GetValidators(networkID uint32) set.Set[ids.NodeID] {
	networkName := constants.NetworkIDToNetworkName[networkID]
	return validatorsPerNetwork[networkName]
}

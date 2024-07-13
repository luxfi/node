// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
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
	//go:embed checkpoints.json
	checkpointsPerNetworkJSON []byte

	checkpointsPerNetwork map[string]map[ids.ID]set.Set[ids.ID]
)

func init() {
	if err := json.Unmarshal(checkpointsPerNetworkJSON, &checkpointsPerNetwork); err != nil {
		panic(fmt.Sprintf("failed to decode checkpoints.json: %v", err))
	}
}

// GetCheckpoints returns all known checkpoints for the chain on the requested
// network.
func GetCheckpoints(networkID uint32, chainID ids.ID) set.Set[ids.ID] {
	networkName := constants.NetworkIDToNetworkName[networkID]
	return checkpointsPerNetwork[networkName][chainID]
}

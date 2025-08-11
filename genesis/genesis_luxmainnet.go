// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	_ "embed"
)

var (
	//go:embed genesis_96369.json
	luxMainnetGenesisConfigJSON []byte

	// LuxMainnetConfig is the Lux mainnet genesis config (network ID 96369)
	LuxMainnetConfig Config
)

func init() {
	// Parse the embedded JSON directly to Config
	config, err := parseGenesisJSONBytesToConfig(luxMainnetGenesisConfigJSON)
	if err != nil {
		panic(err)
	}
	LuxMainnetConfig = *config
}
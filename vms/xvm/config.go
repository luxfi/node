// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"

	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/network"
)

var DefaultConfig = Config{
	Network:          network.DefaultConfig,
	ChecksumsEnabled: true,
	Config: config.Config{
		TxFee:            1000,  // 1000 nanoLux base transaction fee
		CreateAssetTxFee: 10000, // 10000 nanoLux for asset creation
		// xvm execution_root gate OFF by default: a VM with no explicit override
		// keeps the historical empty-root rule. A real network upgrade sets a
		// finite height via the JSON config to activate it. Without this default
		// the Go zero value (0) would activate the path from genesis — see the
		// safety rail in upgrade.MerkleRootNeverActivate.
		MerkleRootActivationHeight: upgrade.MerkleRootNeverActivate,
	},
}

type Config struct {
	Network          network.Config `json:"network"`
	ChecksumsEnabled bool           `json:"checksumsEnabled"`
	config.Config
}

func ParseConfig(configBytes []byte) (Config, error) {
	if len(configBytes) == 0 {
		return DefaultConfig, nil
	}

	cfg := DefaultConfig
	err := json.Unmarshal(configBytes, &cfg, jsonv1.FormatDurationAsNano(true))
	return cfg, err
}

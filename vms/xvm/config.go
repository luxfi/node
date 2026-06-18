// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"

	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/network"
)

var DefaultConfig = Config{
	Network:          network.DefaultConfig,
	ChecksumsEnabled: true,
	Config: config.Config{
		TxFee:            1000,  // 1000 nanoLux base transaction fee
		CreateAssetTxFee: 10000, // 10000 nanoLux for asset creation
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

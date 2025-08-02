// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"encoding/json"

	"github.com/luxfi/node/v2/vms/xvm/network"
)

var DefaultConfig = Config{
	Network:          network.DefaultConfig,
	ChecksumsEnabled: false,
}

type Config struct {
	Network          network.Config `json:"network"`
	ChecksumsEnabled bool           `json:"checksums-enabled"`
}

func ParseConfig(configBytes []byte) (Config, error) {
	if len(configBytes) == 0 {
		return DefaultConfig, nil
	}

	config := DefaultConfig
	err := json.Unmarshal(configBytes, &config)
	return config, err
}

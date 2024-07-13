// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"

	"github.com/luxfi/node/vms/avm/network"
)

var DefaultConfig = Config{
	Network:              network.DefaultConfig,
	IndexTransactions:    false,
	IndexAllowIncomplete: false,
	ChecksumsEnabled:     false,
}

type Config struct {
	Network              network.Config `json:"network"`
	IndexTransactions    bool           `json:"index-transactions"`
	IndexAllowIncomplete bool           `json:"index-allow-incomplete"`
	ChecksumsEnabled     bool           `json:"checksums-enabled"`
}

func ParseConfig(configBytes []byte) (Config, error) {
	if len(configBytes) == 0 {
		return DefaultConfig, nil
	}

	config := DefaultConfig
	err := json.Unmarshal(configBytes, &config)
	return config, err
}

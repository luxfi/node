// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"math/big"

	"github.com/luxfi/geth/common"
)

// TestChainConfig is a minimal chain configuration for testing.
// It avoids importing github.com/luxfi/geth/params to keep test dependencies clean.
type TestChainConfig struct {
	ChainID *big.Int `json:"chainId"`
}

// TestGenesis is a minimal genesis configuration for testing.
// It mirrors core.Genesis but uses TestChainConfig instead of params.ChainConfig.
type TestGenesis struct {
	Config     *TestChainConfig                      `json:"config"`
	Timestamp  uint64                                `json:"timestamp,omitempty"`
	GasLimit   uint64                                `json:"gasLimit"`
	Difficulty *big.Int                              `json:"difficulty"`
	Alloc      map[common.Address]TestGenesisAccount `json:"alloc"`
}

// TestGenesisAccount mirrors core.GenesisAccount
type TestGenesisAccount struct {
	Balance *big.Int `json:"balance"`
}

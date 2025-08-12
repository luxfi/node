package genesis

import (
	_ "embed"
)

// Embed the C-chain genesis configuration for network 96369
//go:embed cchain_genesis_mainnet.json
var CChainGenesisMainnet []byte

// GetCChainGenesisMainnet returns the C-chain genesis configuration for network 96369
func GetCChainGenesisMainnet() string {
	return string(CChainGenesisMainnet)
}

// GetCChainGenesisMainnetBytes returns the C-chain genesis as bytes
func GetCChainGenesisMainnetBytes() []byte {
	return CChainGenesisMainnet
}
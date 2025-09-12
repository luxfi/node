// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"time"

	"github.com/luxfi/ids"
	_ "embed"
)

// LuxGenesisConfig returns the genesis data to use for LUX mainnet
func LuxGenesisConfig() *Config {
	// Parse addresses from hex
	ethAddrBytes, _ := hex.DecodeString("8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	ethAddr, _ := ids.ToShortID(ethAddrBytes)
	
	// LUX address is the same as ETH address for simplicity
	luxAddr := ethAddr
	
	nodeID, _ := ids.NodeIDFromString("NodeID-111111111111111111116DBWJs")
	
	return &Config{
		NetworkID: LuxNetworkID,
		Allocations: []Allocation{
			{
				ETHAddr:       ethAddr,
				LUXAddr:       luxAddr,
				InitialAmount: 1000000000000000, // 1M LUX
				UnlockSchedule: []LockedAmount{},
			},
		},
		StartTime: uint64(time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()),
		InitialStakeDuration: uint64((365 * 24 * time.Hour).Seconds()),
		InitialStakeDurationOffset: 0,
		InitialStakedFunds: []ids.ShortID{
			luxAddr,
		},
		InitialStakers: []Staker{
			{
				NodeID: nodeID,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
		},
		CChainGenesis: `{
			"config": {
				"chainId": 96369,
				"homesteadBlock": 0,
				"eip150Block": 0,
				"eip155Block": 0,
				"eip158Block": 0,
				"byzantiumBlock": 0,
				"constantinopleBlock": 0,
				"petersburgBlock": 0,
				"istanbulBlock": 0,
				"muirGlacierBlock": 0,
				"berlinBlock": 0,
				"londonBlock": 0
			},
			"difficulty": "0x1",
			"gasLimit": "0x1C9C380",
			"alloc": {
				"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
					"balance": "0x21e19e0c9bab2400000"
				}
			}
		}`,
		Message: "LUX Mainnet Genesis",
	}
}
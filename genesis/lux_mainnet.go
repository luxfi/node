// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"time"

	_ "embed"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
)

// LuxGenesisConfig returns the genesis data to use for LUX mainnet
func LuxGenesisConfig() *Config {
	// Parse addresses from hex
	ethAddrBytes, _ := hex.DecodeString("8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	ethAddr, _ := ids.ToShortID(ethAddrBytes)

	// LUX address is the same as ETH address for simplicity
	luxAddr := ethAddr

	// 5 validator nodes for Lux mainnet
	nodeID1, _ := ids.NodeIDFromString("NodeID-Bj4Q9cEsRctbDaeshW2H5SqwEUiu3Yf88")
	nodeID2, _ := ids.NodeIDFromString("NodeID-7epimZACwGDCo8NNN8p4pPbF5caY5CEdq")
	nodeID3, _ := ids.NodeIDFromString("NodeID-KE6LnWkzMgX7kqymggKPcAc8S1q9ipPY9")
	nodeID4, _ := ids.NodeIDFromString("NodeID-PSmbKYD8YaqvvFM2rUSSgMoeszq5FXz2j")
	nodeID5, _ := ids.NodeIDFromString("NodeID-CdUqb58yQQuYYK4ZiEkfjaV2JCYgFnLqU")

	// Create a proper allocation with unlock schedule for staking
	// Each staker will have 1B LUX (1,000,000,000 * 1e9 nLUX) = 5B total
	stakingAmount := uint64(1000000000) * units.Lux // 1B LUX per validator

	return &Config{
		NetworkID: constants.LuxMainnetID,
		Allocations: []Allocation{
			{
				ETHAddr:       ethAddr,
				LUXAddr:       luxAddr,
				InitialAmount: 0, // All funds will be in unlock schedule for staking
				UnlockSchedule: []LockedAmount{
					{
						Amount:   stakingAmount * 5, // 5B total for 5 validators
						Locktime: 0,                 // Available immediately for staking
					},
				},
			},
		},
		StartTime:                  uint64(time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()),
		InitialStakeDuration:       uint64((365 * 24 * time.Hour).Seconds()),
		InitialStakeDurationOffset: 0,
		InitialStakedFunds: []ids.ShortID{
			luxAddr,
		},
		InitialStakers: []Staker{
			{
				NodeID:        nodeID1,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
			{
				NodeID:        nodeID2,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
			{
				NodeID:        nodeID3,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
			{
				NodeID:        nodeID4,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
			{
				NodeID:        nodeID5,
				RewardAddress: luxAddr,
				DelegationFee: 20000, // 2%
			},
		},
		CChainGenesis: string(GetCChainGenesisMainnetBytes()),
		Message: "LUX Mainnet Genesis - 5 Validator Network",
	}
}

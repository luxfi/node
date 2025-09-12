// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import "time"

// LuxNetworkID is the network ID for LUX mainnet
const LuxNetworkID uint32 = 96369

// Genesis represents the platform genesis
type PlatformGenesis struct {
	Timestamp                  uint64
	Allocations                []Allocation
	Message                    string
	NetworkID                  uint32
	InitialStakeDuration       time.Duration
	InitialStakeDurationOffset time.Duration
	InitialStakedFunds         []string
	InitialStakers             []Staker
}
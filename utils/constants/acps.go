// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/luxfi/node/utils/set"

var (
	// ActivatedLPs is the set of LPs that are activated.
	//
	// See: https://github.com/orgs/luxfi/projects/1
	ActivatedLPs = set.Of[uint32](
		23, // https://github.com/luxfi/LPs/blob/main/LPs/23-p-chain-native-transfers/README.md
		24, // https://github.com/luxfi/LPs/blob/main/LPs/24-shanghai-eips/README.md
		25, // https://github.com/luxfi/LPs/blob/main/LPs/25-vm-application-errors/README.md
		30, // https://github.com/luxfi/LPs/blob/main/LPs/30-lux-warp-x-evm/README.md
		31, // https://github.com/luxfi/LPs/blob/main/LPs/31-enable-subnet-ownership-transfer/README.md
		41, // https://github.com/luxfi/LPs/blob/main/LPs/41-remove-pending-stakers/README.md
		62, // https://github.com/luxfi/LPs/blob/main/LPs/62-disable-addvalidatortx-and-adddelegatortx/README.md
	)

	// CurrentLPs is the set of LPs that are currently, at the time of
	// release, marked as implementable and not activated.
	//
	// See: https://github.com/orgs/luxfi/projects/1
	CurrentLPs = set.Of[uint32]()

	// ScheduledLPs are the LPs included into the next upgrade.
	ScheduledLPs = set.Of[uint32]()
)

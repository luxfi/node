// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"net/netip"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/json"
)

type Info struct {
	IP             netip.AddrPort  `json:"ip"`
	PublicIP       netip.AddrPort  `json:"publicIP,omitempty"`
	ID             ids.NodeID      `json:"nodeID"`
	Version        string          `json:"version"`
	LastSent       time.Time       `json:"lastSent"`
	LastReceived   time.Time       `json:"lastReceived"`
	ObservedUptime json.Uint32     `json:"observedUptime"`
	TrackedChains  set.Set[ids.ID] `json:"trackedChains"`
	SupportedLPs   set.Set[uint32] `json:"supportedLPs"`
	ObjectedLPs    set.Set[uint32] `json:"objectedLPs"`
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"net/netip"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/set"
)

type Info struct {
	IP                    netip.AddrPort         `json:"ip"`
	PublicIP              netip.AddrPort         `json:"publicIP,omitempty"`
	ID                    ids.NodeID             `json:"nodeID"`
	Version               string                 `json:"version"`
	LastSent              time.Time              `json:"lastSent"`
	LastReceived          time.Time              `json:"lastReceived"`
	ObservedUptime        json.Uint32            `json:"observedUptime"`
	ObservedSubnetUptimes map[ids.ID]json.Uint32 `json:"observedSubnetUptimes"`
	TrackedSubnets        set.Set[ids.ID]        `json:"trackedSubnets"`
	SupportedLPs          set.Set[uint32]        `json:"supportedLPs"`
	ObjectedLPs           set.Set[uint32]        `json:"objectedLPs"`
}

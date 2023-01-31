// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"time"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/utils/json"
)

type Info struct {
	IP                    string                 `json:"ip"`
	PublicIP              string                 `json:"publicIP,omitempty"`
	ID                    ids.NodeID             `json:"nodeID"`
	Version               string                 `json:"version"`
	LastSent              time.Time              `json:"lastSent"`
	LastReceived          time.Time              `json:"lastReceived"`
	ObservedUptime        json.Uint32            `json:"observedUptime"`
	ObservedSubnetUptimes map[ids.ID]json.Uint32 `json:"observedSubnetUptimes"`
	TrackedSubnets        []ids.ID               `json:"trackedSubnets"`
}

// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/luxfi/node/ids"
)

// subnetIDNodeID = [subnetID] + [nodeID]
const subnetIDNodeIDEntryLength = ids.IDLen + ids.NodeIDLen

var errUnexpectedSubnetIDNodeIDLength = fmt.Errorf("expected subnetID+nodeID entry length %d", subnetIDNodeIDEntryLength)

type subnetIDNodeID struct {
	subnetID ids.ID
	nodeID   ids.NodeID
}

func (s *subnetIDNodeID) Marshal() []byte {
	data := make([]byte, subnetIDNodeIDEntryLength)
	copy(data, s.subnetID[:])
	copy(data[ids.IDLen:], s.nodeID[:])
	return data
}

func (s *subnetIDNodeID) Unmarshal(data []byte) error {
	if len(data) != subnetIDNodeIDEntryLength {
		return errUnexpectedSubnetIDNodeIDLength
	}

	copy(s.subnetID[:], data)
	copy(s.nodeID[:], data[ids.IDLen:])
	return nil
}

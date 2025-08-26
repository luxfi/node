// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/luxfi/ids"
)

// netIDNodeID = [netID] + [nodeID]
const netIDNodeIDEntryLength = ids.IDLen + ids.NodeIDLen

var errUnexpectedNetIDNodeIDLength = fmt.Errorf("expected netID+nodeID entry length %d", netIDNodeIDEntryLength)

type netIDNodeID struct {
	netID ids.ID
	nodeID   ids.NodeID
}

func (s *netIDNodeID) Marshal() []byte {
	data := make([]byte, netIDNodeIDEntryLength)
	copy(data, s.netID[:])
	copy(data[ids.IDLen:], s.nodeID[:])
	return data
}

func (s *netIDNodeID) Unmarshal(data []byte) error {
	if len(data) != netIDNodeIDEntryLength {
		return errUnexpectedNetIDNodeIDLength
	}

	copy(s.netID[:], data)
	copy(s.nodeID[:], data[ids.IDLen:])
	return nil
}

// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/bag"
	"github.com/luxfi/node/utils/formatting"
)

// Set is a collection of polls
type Set interface {
	fmt.Stringer

	Add(requestID uint32, vdrs bag.Bag[ids.NodeID]) bool
	Vote(requestID uint32, vdr ids.NodeID, vote ids.ID) []bag.Bag[ids.ID]
	Drop(requestID uint32, vdr ids.NodeID) []bag.Bag[ids.ID]
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	formatting.PrefixedStringer

	Vote(vdr ids.NodeID, vote ids.ID)
	Drop(vdr ids.NodeID)
	Finished() bool
	Result() bag.Bag[ids.ID]
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs bag.Bag[ids.NodeID]) Poll
}

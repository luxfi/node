// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/utils/formatting"
)

// Set is a collection of polls
type Set interface {
	fmt.Stringer

	Add(requestID uint32, vdrs ids.NodeIDBag) bool
	Vote(requestID uint32, vdr ids.NodeID, vote ids.ID) []ids.Bag
	Drop(requestID uint32, vdr ids.NodeID) []ids.Bag
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	formatting.PrefixedStringer

	Vote(vdr ids.NodeID, vote ids.ID)
	Drop(vdr ids.NodeID)
	Finished() bool
	Result() ids.Bag
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs ids.NodeIDBag) Poll
}

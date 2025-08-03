// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
)

// Context defines the context for proposer block verification
type Context struct {
	// PChainHeight is the height of the decision block on the P-chain
	PChainHeight uint64

	// Timestamp is the time of the block
	Timestamp time.Time

	// BlockID is the ID of the block being verified
	BlockID ids.ID
}
// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/engine/chain/block"
)

// blockAdapter adapts a proposervm Block to implement the engine/chain/block.Block interface
type blockAdapter struct {
	Block // proposervm Block interface
}

func (ba *blockAdapter) Status() choices.Status {
	// Convert uint8 status from proposervm Block to choices.Status
	switch ba.Block.Status() {
	case uint8(choices.Unknown):
		return choices.Unknown
	case uint8(choices.Processing):
		return choices.Processing
	case uint8(choices.Rejected):
		return choices.Rejected
	case uint8(choices.Accepted):
		return choices.Accepted
	default:
		return choices.Unknown
	}
}

// reverseBlockAdapter adapts an engine/chain/block.Block to implement the protocol/chain.Block interface
type reverseBlockAdapter struct {
	block.Block // engine/chain/block.Block interface
}

func (rba *reverseBlockAdapter) Status() uint8 {
	// Convert choices.Status from engine block to uint8
	return uint8(rba.Block.Status())
}
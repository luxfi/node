// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	consChain "github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/vm/rpc/chain"
)

// blockChainAdapter wraps chain.BlockClient to provide uint8 Status() method for consChain.Block
type blockChainAdapter struct {
	*chain.BlockClient
}

// Status returns the block status as uint8
func (b *blockChainAdapter) Status() uint8 {
	return uint8(b.BlockClient.Status())
}

// Ensure blockChainAdapter implements consChain.Block
var _ consChain.Block = (*blockChainAdapter)(nil)

// wrapBlockForChain converts a chain.BlockClient to have the correct Status() signature for consChain.Block
func wrapBlockForChain(bc *chain.BlockClient) consChain.Block {
	if bc == nil {
		return nil
	}
	return &blockChainAdapter{BlockClient: bc}
}

// blockConsensusAdapter wraps chain.BlockClient for consensus interfaces
type blockConsensusAdapter struct {
	*chain.BlockClient
}

// Status returns the block status as uint8 for consensus
func (b *blockConsensusAdapter) Status() uint8 {
	return uint8(b.BlockClient.Status())
}

// Ensure blockConsensusAdapter implements consensus consChain.Block
var _ consChain.Block = (*blockConsensusAdapter)(nil)

// wrapBlockForConsensus converts a chain.BlockClient for consensus interfaces
func wrapBlockForConsensus(bc *chain.BlockClient) consChain.Block {
	if bc == nil {
		return nil
	}
	return &blockConsensusAdapter{BlockClient: bc}
}

package rpcchainvm

import (
	"github.com/luxfi/node/vms/components/chain"
	consChain "github.com/luxfi/consensus/protocol/chain"
)

// blockChainAdapter wraps blockClient to provide uint8 Status() method for chain.Block
type blockChainAdapter struct {
	*blockClient
}

// Status returns the block status as uint8
func (b *blockChainAdapter) Status() uint8 {
	return uint8(b.blockClient.Status())
}

// Ensure blockChainAdapter implements chain.Block
var _ chain.Block = (*blockChainAdapter)(nil)

// wrapBlockForChain converts a blockClient to have the correct Status() signature for chain.Block
func wrapBlockForChain(bc *blockClient) chain.Block {
	if bc == nil {
		return nil
	}
	return &blockChainAdapter{blockClient: bc}
}

// blockConsensusAdapter wraps blockClient for consensus interfaces
type blockConsensusAdapter struct {
	*blockClient
}

// Status returns the block status as uint8 for consensus
func (b *blockConsensusAdapter) Status() uint8 {
	return uint8(b.blockClient.Status())
}

// Ensure blockConsensusAdapter implements consensus chain.Block
var _ consChain.Block = (*blockConsensusAdapter)(nil)

// wrapBlockForConsensus converts a blockClient for consensus interfaces
func wrapBlockForConsensus(bc *blockClient) consChain.Block {
	if bc == nil {
		return nil
	}
	return &blockConsensusAdapter{blockClient: bc}
}
//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	consensuschain "github.com/luxfi/vm/chain"
	vmchain "github.com/luxfi/vm/rpc/chain"
)

// blockChainAdapter wraps vmchain.BlockClient to provide uint8 Status() method for consensuschain.Block
type blockChainAdapter struct {
	*vmchain.BlockClient
}

// Status returns the block status as uint8
func (b *blockChainAdapter) Status() uint8 {
	return uint8(b.BlockClient.Status())
}

// Ensure blockChainAdapter implements consensuschain.Block
var _ consensuschain.Block = (*blockChainAdapter)(nil)

// wrapBlockForChain converts a vmchain.BlockClient to have the correct Status() signature for consensuschain.Block
func wrapBlockForChain(bc *vmchain.BlockClient) consensuschain.Block {
	if bc == nil {
		return nil
	}
	return &blockChainAdapter{BlockClient: bc}
}

// blockConsensusAdapter wraps vmchain.BlockClient for consensus interfaces
type blockConsensusAdapter struct {
	*vmchain.BlockClient
}

// Status returns the block status as uint8 for consensus
func (b *blockConsensusAdapter) Status() uint8 {
	return uint8(b.BlockClient.Status())
}

// Ensure blockConsensusAdapter implements consensuschain.Block
var _ consensuschain.Block = (*blockConsensusAdapter)(nil)

// wrapBlockForConsensus converts a vmchain.BlockClient for consensus interfaces
func wrapBlockForConsensus(bc *vmchain.BlockClient) consensuschain.Block {
	if bc == nil {
		return nil
	}
	return &blockConsensusAdapter{BlockClient: bc}
}

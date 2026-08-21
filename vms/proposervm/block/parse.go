// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

type ParseResult struct {
	Block Block
	Err   error
}

// ParseBlocks parses the given raw blocks into tuples of (Block, error).
// Each ParseResult is returned in the same order as its corresponding bytes in the input.
func ParseBlocks(blks [][]byte, chainID ids.ID) []ParseResult {
	results := make([]ParseResult, len(blks))

	var wg sync.WaitGroup
	wg.Add(len(blks))

	for i, blk := range blks {
		go func(i int, blkBytes []byte) {
			defer wg.Done()
			results[i].Block, results[i].Err = Parse(blkBytes, chainID)
		}(i, blk)
	}

	wg.Wait()

	return results
}

// Parse a block and verify that the signature attached to the block is valid
// for the certificate provided in the block.
func Parse(bytes []byte, chainID ids.ID) (Block, error) {
	block, err := ParseWithoutVerification(bytes)
	if err != nil {
		return nil, err
	}
	return block, block.verify(chainID)
}

// ParseWithoutVerification parses a block without verifying that the signature
// on the block is correct. Dispatch is the 1-byte blockKind at object offset 0
// of the leading self-delimiting zap message — no codec, no version.
func ParseWithoutVerification(bytes []byte) (Block, error) {
	n, err := zapLen(bytes)
	if err != nil {
		return nil, err
	}
	msg, err := zap.Parse(bytes[:n])
	if err != nil {
		return nil, err
	}

	switch k := blockKind(msg.Root().Uint8(offKind)); k {
	case blkSigned:
		block := &statelessBlock{}
		return block, block.initialize(bytes)
	case blkOption:
		if n != len(bytes) {
			return nil, errNonCanonicalWire
		}
		block := &option{}
		return block, block.initialize(bytes)
	default:
		return nil, fmt.Errorf("%w: %d", errUnknownBlockType, k)
	}
}

func ParseHeader(bytes []byte) (Header, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	return &statelessHeader{
		Chain:  ids.ID(read32(root, hdrChain)),
		Parent: ids.ID(read32(root, hdrParent)),
		Body:   ids.ID(read32(root, hdrBody)),
		bytes:  bytes,
	}, nil
}

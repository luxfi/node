// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaintest

import (
	"context"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
)

// Block interface matches chain.Block to avoid import cycle
type Block interface {
	choices.Decidable
	Parent() ids.ID
	Height() uint64
	Time() uint64
	Verify(context.Context) error
	Bytes() []byte
	Accept() error
}

var (
	// GenesisID is the ID of the genesis block
	GenesisID = ids.GenerateTestID()
	
	// GenesisBytes is the byte representation of genesis
	GenesisBytes = GenesisID[:]
	
	// GenesisTimestamp is the timestamp of genesis
	GenesisTimestamp = uint64(0)
	
	// Genesis is the genesis block
	Genesis = &TestBlock{
		IDV:     GenesisID,
		ParentV: ids.Empty,
		HeightV: 0,
		TimeV:   0,
		StatusV: choices.Accepted,
		BytesV:  GenesisBytes,
	}
)

// BuildChild builds a child block of the given parent
func BuildChild(parent Block) *TestBlock {
	// Handle both TestBlock and generic Block
	var parentID ids.ID
	if tb, ok := parent.(*TestBlock); ok {
		parentID = tb.IDV
	} else {
		// For generic blocks, parse the ID string back to ids.ID
		parentID, _ = ids.FromString(parent.ID())
	}
	
	newID := ids.GenerateTestID()
	return &TestBlock{
		IDV:     newID,
		ParentV: parentID,
		HeightV: parent.Height() + 1,
		TimeV:   parent.Time() + 1,
		StatusV: choices.Processing,
		BytesV:  newID[:],
	}
}

// BuildLinear builds a linear chain of blocks
func BuildLinear(parent Block, count int) []*TestBlock {
	blocks := make([]*TestBlock, count)
	current := parent
	for i := 0; i < count; i++ {
		blocks[i] = BuildChild(current)
		current = blocks[i]
	}
	return blocks
}

// MakeLastAcceptedBlockF creates a function that returns the last accepted block
func MakeLastAcceptedBlockF(blocks []*TestBlock, opts []*TestBlock) func() (string, error) {
	return func() (string, error) {
		if len(opts) > 0 && opts[0].StatusV == choices.Accepted {
			return opts[0].ID(), nil
		}
		if len(blocks) > 0 {
			return blocks[len(blocks)-1].ID(), nil
		}
		return Genesis.ID(), nil
	}
}
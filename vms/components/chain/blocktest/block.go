// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/chain"
	"github.com/luxfi/node/vms/components/state"
)

var (
	GenesisID        = ids.GenerateTestID()
	GenesisHeight    = uint64(0)
	GenesisTimestamp = time.Unix(0, 0)
	GenesisBytes     = []byte("genesis")
	
	Genesis = &Block{
		TestBlock: chain.TestBlock{
			IDV:        GenesisID,
			HeightV:    GenesisHeight,
			TimestampV: GenesisTimestamp,
			ParentV:    ids.Empty,
			BytesV:     GenesisBytes,
			StatusV:    chain.Accepted,
		},
	}
	
	nextID = uint64(1)
)

// Block is a test block that implements chain.Block
type Block struct {
	chain.TestBlock
	state state.ReadOnlyChain
}

func (b *Block) ID() ids.ID {
	return b.IDV
}

func (b *Block) Height() uint64 {
	return b.HeightV
}

func (b *Block) Timestamp() time.Time {
	return b.TimestampV
}

func (b *Block) Parent() ids.ID {
	return b.ParentV
}

func (b *Block) Bytes() []byte {
	return b.BytesV
}

func (b *Block) Verify(context.Context) error {
	if !b.ShouldVerifyV {
		return b.ErrV
	}
	return nil
}

func (b *Block) Accept(context.Context) error {
	b.StatusV = chain.Accepted
	return b.ErrV
}

func (b *Block) Reject(context.Context) error {
	b.StatusV = chain.Rejected
	return b.ErrV
}

func (b *Block) Status() chain.Status {
	return b.StatusV
}

func (b *Block) State() state.ReadOnlyChain {
	return b.state
}

func (b *Block) SetStatus(status choices.Status) {
	// Convert choices.Status to chain.Status
	switch status {
	case choices.Unknown:
		b.StatusV = chain.Unknown
	case choices.Processing:
		b.StatusV = chain.Processing
	case choices.Rejected:
		b.StatusV = chain.Rejected
	case choices.Accepted:
		b.StatusV = chain.Accepted
	}
}

// BuildChild creates a child block of the given parent
func BuildChild(parent chain.Block) *Block {
	nextID++
	blockID := ids.ID{}
	copy(blockID[:], fmt.Sprintf("block_%d", nextID))
	
	// Get parent timestamp if available
	var timestamp time.Time
	if testParent, ok := parent.(*chain.TestBlock); ok {
		timestamp = testParent.Timestamp().Add(time.Second)
	} else if blockParent, ok := parent.(*Block); ok {
		timestamp = blockParent.Timestamp().Add(time.Second)
	} else {
		// Default to current time if parent doesn't have timestamp
		timestamp = time.Now()
	}
	
	return &Block{
		TestBlock: chain.TestBlock{
			IDV:        blockID,
			HeightV:    parent.Height() + 1,
			TimestampV: timestamp,
			ParentV:    parent.ID(),
			BytesV:     []byte(fmt.Sprintf("block_%d", nextID)),
			StatusV:    chain.Processing,
			ShouldVerifyV: true,
		},
	}
}
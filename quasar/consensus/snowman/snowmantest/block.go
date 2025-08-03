// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package snowmantest provides test utilities for snowman consensus
package snowmantest

import (
	"context"
	"time"

	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/ids"
)

// Block is a test implementation of snowman.Block
type Block struct {
	choices.TestDecidable

	IDV         ids.ID
	ParentV     ids.ID
	HeightV     uint64
	TimestampV  time.Time
	VerifyV     error
	BytesV      []byte
}

// ID implements snowman.Block
func (b *Block) ID() ids.ID {
	return b.IDV
}

// Parent implements snowman.Block
func (b *Block) Parent() ids.ID {
	return b.ParentV
}

// Height implements snowman.Block
func (b *Block) Height() uint64 {
	return b.HeightV
}

// Timestamp implements snowman.Block
func (b *Block) Timestamp() time.Time {
	return b.TimestampV
}

// Verify implements snowman.Block
func (b *Block) Verify(context.Context) error {
	return b.VerifyV
}

// Bytes implements snowman.Block
func (b *Block) Bytes() []byte {
	return b.BytesV
}

// Accept implements choices.Decidable
func (b *Block) Accept(context.Context) error {
	b.TestDecidable.StatusV = choices.Accepted
	return nil
}

// Reject implements choices.Decidable
func (b *Block) Reject(context.Context) error {
	b.TestDecidable.StatusV = choices.Rejected
	return nil
}
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/choices"
)

// TestVertex is a test implementation of a vertex.
type TestVertex struct {
	TestDecidable
	ParentsV []ids.ID
	HeightV  uint64
	EpochV   uint32
	VerifyV  error
	BytesV   []byte
}

// Vertex returns the ID of this vertex.
func (v *TestVertex) Vertex() ids.ID {
	return v.IDV
}

// Parents returns the parents of this vertex.
func (v *TestVertex) Parents() []ids.ID {
	return v.ParentsV
}

// Height returns the height of this vertex.
func (v *TestVertex) Height() uint64 {
	return v.HeightV
}

// Epoch returns the epoch of this vertex.
func (v *TestVertex) Epoch() uint32 {
	return v.EpochV
}

// Verify returns the verification error.
func (v *TestVertex) Verify(context.Context) error {
	return v.VerifyV
}

// Bytes returns the byte representation.
func (v *TestVertex) Bytes() []byte {
	return v.BytesV
}

// TestDecidable is a test implementation of choices.Decidable.
type TestDecidable struct {
	IDV        ids.ID
	AcceptV    error
	RejectV    error
	StatusV    choices.Status
}

// ID returns the ID of this element.
func (d *TestDecidable) ID() string {
	return d.IDV.String()
}

// Accept accepts this element.
func (d *TestDecidable) Accept(context.Context) error {
	if d.AcceptV != nil {
		return d.AcceptV
	}
	d.StatusV = choices.Accepted
	return nil
}

// Reject rejects this element.
func (d *TestDecidable) Reject(context.Context) error {
	if d.RejectV != nil {
		return d.RejectV
	}
	d.StatusV = choices.Rejected
	return nil
}

// Status returns the status of this element.
func (d *TestDecidable) Status() choices.Status {
	return d.StatusV
}

// TestBlock is a test implementation of a block.
type TestBlock struct {
	TestDecidable
	ParentV   ids.ID
	HeightV   uint64
	TimeV     uint64
	VerifyV   error
	BytesV    []byte
}

// Parent returns the parent block ID.
func (b *TestBlock) Parent() ids.ID {
	return b.ParentV
}

// Height returns the height of the block.
func (b *TestBlock) Height() uint64 {
	return b.HeightV
}

// Time returns the time the block was created.
func (b *TestBlock) Time() uint64 {
	return b.TimeV
}

// Verify returns the verification error.
func (b *TestBlock) Verify(context.Context) error {
	return b.VerifyV
}

// Bytes returns the byte representation.
func (b *TestBlock) Bytes() []byte {
	return b.BytesV
}

// VertexFactory creates test vertices.
type VertexFactory struct {
	nextID uint64
}

// NewVertexFactory creates a new vertex factory.
func NewVertexFactory() *VertexFactory {
	return &VertexFactory{}
}

// New creates a new test vertex.
func (f *VertexFactory) New(parents []ids.ID) *TestVertex {
	vtxID := ids.ID{}
	vtxID[0] = byte(f.nextID)
	f.nextID++
	
	return &TestVertex{
		TestDecidable: TestDecidable{
			IDV:     vtxID,
			StatusV: choices.Processing,
		},
		ParentsV: parents,
		HeightV:  f.nextID,
	}
}

// BlockFactory creates test blocks.
type BlockFactory struct {
	nextID uint64
}

// NewBlockFactory creates a new block factory.
func NewBlockFactory() *BlockFactory {
	return &BlockFactory{}
}

// New creates a new test block.
func (f *BlockFactory) New(parent ids.ID) *TestBlock {
	blkID := ids.ID{}
	blkID[0] = byte(f.nextID)
	f.nextID++
	
	return &TestBlock{
		TestDecidable: TestDecidable{
			IDV:     blkID,
			StatusV: choices.Processing,
		},
		ParentV: parent,
		HeightV: f.nextID,
		TimeV:   uint64(f.nextID * 1000),
	}
}

// ErrTest is a test error.
var ErrTest = errors.New("test error")
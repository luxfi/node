// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/quasar/engine/chain/block"
)

// TestBlock is a test implementation of block.Block
type TestBlock struct {
	TestDecidable
	HeightV uint64
	TimeV   uint64
	VerifyV error
	BytesV  []byte
}

func (b *TestBlock) Height() uint64              { return b.HeightV }
func (b *TestBlock) Time() uint64                { return b.TimeV }
func (b *TestBlock) Verify(context.Context) error { return b.VerifyV }
func (b *TestBlock) Bytes() []byte               { return b.BytesV }

// TestDecidable is embedded in TestBlock to implement choices.Decidable.
type TestDecidable struct {
	IDV       ids.ID
	AcceptV   error
	RejectV   error
	StatusV   choices.Status
	ParentV   ids.ID
}

func (d *TestDecidable) ID() ids.ID            { return d.IDV }
func (d *TestDecidable) Accept() error         { return d.AcceptV }
func (d *TestDecidable) Reject() error         { return d.RejectV }
func (d *TestDecidable) Status() choices.Status { return d.StatusV }
func (d *TestDecidable) Parent() ids.ID        { return d.ParentV }

// TestVM is a test implementation of block.ChainVM
type TestVM struct {
	CantBuildBlock
	CantParseBlock
	CantGetBlock
	CantSetPreference
	CantLastAccepted

	BuildBlockF    func(context.Context) (block.Block, error)
	ParseBlockF    func(context.Context, []byte) (block.Block, error)
	GetBlockF      func(context.Context, ids.ID) (block.Block, error)
	SetPreferenceF func(context.Context, ids.ID) error
	LastAcceptedF  func(context.Context) (ids.ID, error)
}

func (vm *TestVM) BuildBlock(ctx context.Context) (block.Block, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	return vm.CantBuildBlock.BuildBlock(ctx)
}

func (vm *TestVM) ParseBlock(ctx context.Context, b []byte) (block.Block, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, b)
	}
	return vm.CantParseBlock.ParseBlock(ctx, b)
}

func (vm *TestVM) GetBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, blkID)
	}
	return vm.CantGetBlock.GetBlock(ctx, blkID)
}

func (vm *TestVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	if vm.SetPreferenceF != nil {
		return vm.SetPreferenceF(ctx, blkID)
	}
	return vm.CantSetPreference.SetPreference(ctx, blkID)
}

func (vm *TestVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	return vm.CantLastAccepted.LastAccepted(ctx)
}

// Cant* types provide default implementations that panic

type CantBuildBlock struct{}
func (CantBuildBlock) BuildBlock(context.Context) (block.Block, error) {
	panic("BuildBlock called unexpectedly")
}

type CantParseBlock struct{}
func (CantParseBlock) ParseBlock(context.Context, []byte) (block.Block, error) {
	panic("ParseBlock called unexpectedly")
}

type CantGetBlock struct{}
func (CantGetBlock) GetBlock(context.Context, ids.ID) (block.Block, error) {
	panic("GetBlock called unexpectedly")
}

type CantSetPreference struct{}
func (CantSetPreference) SetPreference(context.Context, ids.ID) error {
	panic("SetPreference called unexpectedly")
}

type CantLastAccepted struct{}
func (CantLastAccepted) LastAccepted(context.Context) (ids.ID, error) {
	panic("LastAccepted called unexpectedly")
}

// Helper functions

// MakeBlock creates a new test block
func MakeBlock(
	id ids.ID,
	parent ids.ID,
	height uint64,
	timestamp uint64,
	status choices.Status,
) *TestBlock {
	return &TestBlock{
		TestDecidable: TestDecidable{
			IDV:     id,
			ParentV: parent,
			StatusV: status,
		},
		HeightV: height,
		TimeV:   timestamp,
		BytesV:  id[:],
	}
}

// MakeBlockWithVerifyError creates a block that returns an error on verify
func MakeBlockWithVerifyError(
	id ids.ID,
	parent ids.ID,
	height uint64,
	timestamp uint64,
	status choices.Status,
	verifyErr error,
) *TestBlock {
	blk := MakeBlock(id, parent, height, timestamp, status)
	blk.VerifyV = verifyErr
	return blk
}

// AssertBlockEqual asserts that two blocks are equal
func AssertBlockEqual(t *testing.T, expected, actual block.Block) {
	require.Equal(t, expected.ID(), actual.ID())
	require.Equal(t, expected.Parent(), actual.Parent())
	require.Equal(t, expected.Height(), actual.Height())
	require.Equal(t, expected.Time(), actual.Time())
	require.Equal(t, expected.Bytes(), actual.Bytes())
}
// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package chaintest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
)

// Parameters matches Parameters to avoid import cycle
type Parameters struct {
	K                     int
	AlphaPreference       int
	AlphaConfidence       int
	Beta                  int
	ConcurrentRepolls     int
	OptimalProcessing     int
	MaxOutstandingItems   int
	MaxItemProcessingTime int64
}

// TestConsensus is a test implementation of chain.Consensus
type TestConsensus struct {
	T *testing.T

	CantInitialize
	CantAdd
	CantRecordPoll

	InitializeF      func(context.Context, Parameters, string, uint64, uint64) error
	ParametersF      func() Parameters
	AddF             func(context.Context, Block) error
	IssuedF          func(Block) bool
	ProcessingF      func(ids.ID) bool
	DecidedF         func(Block) bool
	IsPreferredF     func(Block) bool
	PreferenceF      func() ids.ID
	RecordPollF      func(context.Context, []ids.ID) error
	FinalizedF       func() bool
	HealthCheckF     func(context.Context) (interface{}, error)
	NumProcessingF   func() int
}

func (c *TestConsensus) Initialize(ctx context.Context, params Parameters, lastAcceptedID string, lastAcceptedHeight uint64, lastAcceptedTime uint64) error {
	if c.InitializeF != nil {
		return c.InitializeF(ctx, params, lastAcceptedID, lastAcceptedHeight, lastAcceptedTime)
	}
	return c.CantInitialize.Initialize(ctx, params, lastAcceptedID, lastAcceptedHeight, lastAcceptedTime)
}

func (c *TestConsensus) Parameters() Parameters {
	if c.ParametersF != nil {
		return c.ParametersF()
	}
	require.FailNow(c.T, "Parameters not implemented")
	return Parameters{}
}

func (c *TestConsensus) Add(ctx context.Context, blk Block) error {
	if c.AddF != nil {
		return c.AddF(ctx, blk)
	}
	return c.CantAdd.Add(ctx, blk)
}

func (c *TestConsensus) Issued(blk Block) bool {
	if c.IssuedF != nil {
		return c.IssuedF(blk)
	}
	require.FailNow(c.T, "Issued not implemented")
	return false
}

func (c *TestConsensus) Processing(blkID ids.ID) bool {
	if c.ProcessingF != nil {
		return c.ProcessingF(blkID)
	}
	require.FailNow(c.T, "Processing not implemented")
	return false
}

func (c *TestConsensus) Decided(blk Block) bool {
	if c.DecidedF != nil {
		return c.DecidedF(blk)
	}
	require.FailNow(c.T, "Decided not implemented")
	return false
}

func (c *TestConsensus) IsPreferred(blk Block) bool {
	if c.IsPreferredF != nil {
		return c.IsPreferredF(blk)
	}
	require.FailNow(c.T, "IsPreferred not implemented")
	return false
}

func (c *TestConsensus) Preference() ids.ID {
	if c.PreferenceF != nil {
		return c.PreferenceF()
	}
	require.FailNow(c.T, "Preference not implemented")
	return ids.Empty
}

func (c *TestConsensus) RecordPoll(ctx context.Context, votes []ids.ID) error {
	if c.RecordPollF != nil {
		return c.RecordPollF(ctx, votes)
	}
	return c.CantRecordPoll.RecordPoll(ctx, votes)
}

func (c *TestConsensus) Finalized() bool {
	if c.FinalizedF != nil {
		return c.FinalizedF()
	}
	require.FailNow(c.T, "Finalized not implemented")
	return false
}

func (c *TestConsensus) HealthCheck(ctx context.Context) (interface{}, error) {
	if c.HealthCheckF != nil {
		return c.HealthCheckF(ctx)
	}
	return nil, nil
}

func (c *TestConsensus) NumProcessing() int {
	if c.NumProcessingF != nil {
		return c.NumProcessingF()
	}
	return 0
}

// Cant* types provide default implementations that panic

type CantInitialize struct{}
func (CantInitialize) Initialize(context.Context, Parameters, string, uint64, uint64) error {
	panic("Initialize called unexpectedly")
}

type CantAdd struct{}
func (CantAdd) Add(context.Context, Block) error {
	panic("Add called unexpectedly")
}

type CantRecordPoll struct{}
func (CantRecordPoll) RecordPoll(context.Context, []ids.ID) error {
	panic("RecordPoll called unexpectedly")
}

// TestBlock is a test implementation of Block
type TestBlock struct {
	IDV       ids.ID
	AcceptV   error
	RejectV   error
	StatusV   choices.Status
	ParentV   ids.ID
	HeightV   uint64
	TimeV     uint64
	VerifyV   error
	BytesV    []byte
}

func (b *TestBlock) ID() string              { return b.IDV.String() }
func (b *TestBlock) Accept() error           { 
	if b.AcceptV == nil {
		b.StatusV = choices.Accepted
	}
	return b.AcceptV 
}
func (b *TestBlock) Reject() error           { 
	if b.RejectV == nil {
		b.StatusV = choices.Rejected
	}
	return b.RejectV 
}
func (b *TestBlock) Status() choices.Status  { return b.StatusV }
func (b *TestBlock) Parent() ids.ID          { return b.ParentV }
func (b *TestBlock) Height() uint64          { return b.HeightV }
func (b *TestBlock) Time() uint64            { return b.TimeV }
func (b *TestBlock) Verify(context.Context) error { return b.VerifyV }
func (b *TestBlock) Bytes() []byte           { return b.BytesV }

// MakeTestBlock creates a new test block
func MakeTestBlock(
	id ids.ID,
	parent ids.ID,
	height uint64,
	timestamp uint64,
	status choices.Status,
) *TestBlock {
	return &TestBlock{
		IDV:     id,
		ParentV: parent,
		HeightV: height,
		TimeV:   timestamp,
		StatusV: status,
		BytesV:  id[:],
	}
}
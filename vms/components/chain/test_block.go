// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"time"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/state"
)

// TestBlock is a test implementation of Block
type TestBlock struct {
	IDV        ids.ID
	HeightV    uint64
	TimestampV time.Time
	ParentV    ids.ID
	BytesV     []byte
	StatusV    Status
	ErrV       error
	ShouldVerifyV bool
}

func (b *TestBlock) ID() ids.ID {
	return b.IDV
}

func (b *TestBlock) Height() uint64 {
	return b.HeightV
}

func (b *TestBlock) Timestamp() time.Time {
	return b.TimestampV
}

func (b *TestBlock) Parent() ids.ID {
	return b.ParentV
}

func (b *TestBlock) Bytes() []byte {
	return b.BytesV
}

func (b *TestBlock) Verify(context.Context) error {
	if !b.ShouldVerifyV {
		return b.ErrV
	}
	return nil
}

func (b *TestBlock) Accept(context.Context) error {
	b.StatusV = Accepted
	return b.ErrV
}

func (b *TestBlock) Reject(context.Context) error {
	b.StatusV = Rejected
	return b.ErrV
}

func (b *TestBlock) Status() Status {
	return b.StatusV
}

func (b *TestBlock) State() state.ReadOnlyChain {
	return nil
}

func (b *TestBlock) SetStatus(status choices.Status) {
	// Convert choices.Status to chain.Status
	switch status {
	case choices.Unknown:
		b.StatusV = Unknown
	case choices.Processing:
		b.StatusV = Processing
	case choices.Rejected:
		b.StatusV = Rejected
	case choices.Accepted:
		b.StatusV = Accepted
	}
}
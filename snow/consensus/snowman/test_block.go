// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils"
)

var (
	_ Block                      = (*TestBlock)(nil)
	_ utils.Sortable[*TestBlock] = (*TestBlock)(nil)
)

// TestBlock is a useful test block
type TestBlock struct {
	choices.TestDecidable

	ParentV    ids.ID
	HeightV    uint64
	TimestampV time.Time
	VerifyV    error
	BytesV     []byte
}

func (b *TestBlock) Parent() ids.ID {
	return b.ParentV
}

func (b *TestBlock) Height() uint64 {
	return b.HeightV
}

func (b *TestBlock) Timestamp() time.Time {
	return b.TimestampV
}

func (b *TestBlock) Verify(context.Context) error {
	return b.VerifyV
}

func (b *TestBlock) Bytes() []byte {
	return b.BytesV
}

func (b *TestBlock) Compare(other *TestBlock) int {
	if b.HeightV < other.HeightV {
		return -1
	}
	if b.HeightV > other.HeightV {
		return 1
	}
	return 0
}

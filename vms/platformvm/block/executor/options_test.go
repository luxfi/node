// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/snow/consensus/snowman"
	"github.com/luxdefi/node/vms/platformvm/block"
)

func TestOptionsUnexpectedBlockType(t *testing.T) {
	tests := []block.Block{
		&block.BanffAbortBlock{},
		&block.BanffCommitBlock{},
		&block.BanffStandardBlock{},
		&block.ApricotAbortBlock{},
		&block.ApricotCommitBlock{},
		&block.ApricotStandardBlock{},
		&block.ApricotAtomicBlock{},
	}

	for _, blk := range tests {
		t.Run(fmt.Sprintf("%T", blk), func(t *testing.T) {
			err := blk.Visit(&options{})
			require.ErrorIs(t, err, snowman.ErrNotOracle)
		})
	}
}

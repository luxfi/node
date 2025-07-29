// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaintest

import (
	"context"

	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/db"
	"github.com/luxfi/ids"
)

func MakeLastAcceptedBlockF(blks ...[]*Block) func(context.Context) (ids.ID, error) {
	return func(context.Context) (ids.ID, error) {
		var (
			highestHeight uint64
			highestID     ids.ID
		)
		for _, blkSlice := range blks {
			for _, blk := range blkSlice {
				if blk.Status != consensustest.Accepted {
					continue
				}

				if height := blk.Height(); height >= highestHeight {
					highestHeight = height
					highestID = blk.ID()
				}
			}
		}
		return highestID, nil
	}
}

func MakeGetBlockIDAtHeightF(blks ...[]*Block) func(context.Context, uint64) (ids.ID, error) {
	return func(_ context.Context, height uint64) (ids.ID, error) {
		for _, blkSlice := range blks {
			for _, blk := range blkSlice {
				if blk.Status != consensustest.Accepted {
					continue
				}

				if height == blk.Height() {
					return blk.ID(), nil
				}
			}
		}
		return ids.Empty, database.ErrNotFound
	}
}

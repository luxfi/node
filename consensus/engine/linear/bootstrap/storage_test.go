// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/db"
	"github.com/luxfi/db/memdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/linear/lineartest"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/engine/linear/block"
	"github.com/luxfi/node/consensus/engine/linear/bootstrap/interval"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
)

var _ block.Parser = testParser(nil)

func TestGetMissingBlockIDs(t *testing.T) {
	blocks := lineartest.BuildLinear(7)
	parser := makeParser(blocks)

	tests := []struct {
		name               string
		blocks             []linear.Block
		lastAcceptedHeight uint64
		expected           set.Set[ids.ID]
	}{
		{
			name:               "initially empty",
			blocks:             nil,
			lastAcceptedHeight: 0,
			expected:           nil,
		},
		{
			name:               "wants one block",
			blocks:             []linear.Block{blocks[4]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[3].ID()),
		},
		{
			name:               "wants multiple blocks",
			blocks:             []linear.Block{blocks[2], blocks[4]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[1].ID(), blocks[3].ID()),
		},
		{
			name:               "doesn't want last accepted block",
			blocks:             []linear.Block{blocks[1]},
			lastAcceptedHeight: 0,
			expected:           nil,
		},
		{
			name:               "doesn't want known block",
			blocks:             []linear.Block{blocks[2], blocks[3]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[1].ID()),
		},
		{
			name:               "doesn't want already accepted block",
			blocks:             []linear.Block{blocks[1]},
			lastAcceptedHeight: 4,
			expected:           nil,
		},
		{
			name:               "doesn't underflow",
			blocks:             []linear.Block{blocks[0]},
			lastAcceptedHeight: 0,
			expected:           nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)
			for _, blk := range test.blocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			missingBlockIDs, err := getMissingBlockIDs(
				context.Background(),
				db,
				parser,
				tree,
				test.lastAcceptedHeight,
			)
			require.NoError(err)
			require.Equal(test.expected, missingBlockIDs)
		})
	}
}

func TestProcess(t *testing.T) {
	blocks := lineartest.BuildLinear(7)

	tests := []struct {
		name                        string
		initialBlocks               []linear.Block
		lastAcceptedHeight          uint64
		missingBlockIDs             set.Set[ids.ID]
		blk                         linear.Block
		ancestors                   map[ids.ID]linear.Block
		expectedParentID            ids.ID
		expectedShouldFetchParentID bool
		expectedMissingBlockIDs     set.Set[ids.ID]
		expectedTrackedHeights      []uint64
	}{
		{
			name:                        "add single block",
			initialBlocks:               nil,
			lastAcceptedHeight:          0,
			missingBlockIDs:             set.Of(blocks[5].ID()),
			blk:                         blocks[5],
			ancestors:                   nil,
			expectedParentID:            blocks[4].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{5},
		},
		{
			name:               "add multiple blocks",
			initialBlocks:      nil,
			lastAcceptedHeight: 0,
			missingBlockIDs:    set.Of(blocks[5].ID()),
			blk:                blocks[5],
			ancestors: map[ids.ID]linear.Block{
				blocks[4].ID(): blocks[4],
			},
			expectedParentID:            blocks[3].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{4, 5},
		},
		{
			name:               "ignore non-consecutive blocks",
			initialBlocks:      nil,
			lastAcceptedHeight: 0,
			missingBlockIDs:    set.Of(blocks[3].ID(), blocks[5].ID()),
			blk:                blocks[5],
			ancestors: map[ids.ID]linear.Block{
				blocks[3].ID(): blocks[3],
			},
			expectedParentID:            blocks[4].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Of(blocks[3].ID()),
			expectedTrackedHeights:      []uint64{5},
		},
		{
			name:                        "do not request the last accepted block",
			initialBlocks:               nil,
			lastAcceptedHeight:          2,
			missingBlockIDs:             set.Of(blocks[3].ID()),
			blk:                         blocks[3],
			ancestors:                   nil,
			expectedParentID:            ids.Empty,
			expectedShouldFetchParentID: false,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{3},
		},
		{
			name:                        "do not request already known block",
			initialBlocks:               []linear.Block{blocks[2]},
			lastAcceptedHeight:          0,
			missingBlockIDs:             set.Of(blocks[1].ID(), blocks[3].ID()),
			blk:                         blocks[3],
			ancestors:                   nil,
			expectedParentID:            ids.Empty,
			expectedShouldFetchParentID: false,
			expectedMissingBlockIDs:     set.Of(blocks[1].ID()),
			expectedTrackedHeights:      []uint64{2, 3},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)
			for _, blk := range test.initialBlocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			parentID, shouldFetchParentID, err := process(
				db,
				tree,
				test.missingBlockIDs,
				test.lastAcceptedHeight,
				test.blk,
				test.ancestors,
			)
			require.NoError(err)
			require.Equal(test.expectedShouldFetchParentID, shouldFetchParentID)
			require.Equal(test.expectedParentID, parentID)
			require.Equal(test.expectedMissingBlockIDs, test.missingBlockIDs)

			require.Equal(uint64(len(test.expectedTrackedHeights)), tree.Len())
			for _, height := range test.expectedTrackedHeights {
				require.True(tree.Contains(height))
			}
		})
	}
}

func TestExecute(t *testing.T) {
	const numBlocks = 7

	unhalted := &core.Halter{}
	halted := &core.Halter{}
	halted.Halt()

	tests := []struct {
		name                      string
		haltable                  core.Haltable
		lastAcceptedHeight        uint64
		expectedProcessingHeights []uint64
		expectedAcceptedHeights   []uint64
	}{
		{
			name:                      "execute everything",
			haltable:                  unhalted,
			lastAcceptedHeight:        0,
			expectedProcessingHeights: nil,
			expectedAcceptedHeights:   []uint64{0, 1, 2, 3, 4, 5, 6},
		},
		{
			name:                      "do not execute blocks accepted by height",
			haltable:                  unhalted,
			lastAcceptedHeight:        3,
			expectedProcessingHeights: []uint64{1, 2, 3},
			expectedAcceptedHeights:   []uint64{0, 4, 5, 6},
		},
		{
			name:                      "do not execute blocks when halted",
			haltable:                  halted,
			lastAcceptedHeight:        0,
			expectedProcessingHeights: []uint64{1, 2, 3, 4, 5, 6},
			expectedAcceptedHeights:   []uint64{0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)

			blocks := lineartest.BuildLinear(numBlocks)
			parser := makeParser(blocks)
			for _, blk := range blocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			require.NoError(execute(
				context.Background(),
				test.haltable.Halted,
				logging.NoLog{}.Info,
				db,
				parser,
				tree,
				test.lastAcceptedHeight,
			))
			for _, height := range test.expectedProcessingHeights {
				require.Equal(consensustest.Undecided, blocks[height].Status)
			}
			for _, height := range test.expectedAcceptedHeights {
				require.Equal(consensustest.Accepted, blocks[height].Status)
			}

			if test.haltable.Halted() {
				return
			}

			size, err := database.Count(db)
			require.NoError(err)
			require.Zero(size)
		})
	}
}

type testParser func(context.Context, []byte) (linear.Block, error)

func (f testParser) ParseBlock(ctx context.Context, bytes []byte) (linear.Block, error) {
	return f(ctx, bytes)
}

func makeParser(blocks []*lineartest.Block) block.Parser {
	return testParser(func(_ context.Context, b []byte) (linear.Block, error) {
		for _, block := range blocks {
			if bytes.Equal(b, block.Bytes()) {
				return block, nil
			}
		}
		return nil, database.ErrNotFound
	})
}

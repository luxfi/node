// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"path"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"

	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/chain/chaintest"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/bag"
	"github.com/prometheus/client_golang/prometheus"
)

type testFunc func(*testing.T, Factory)

var (
	testFuncs = []testFunc{
		InitializeTest,
		NumProcessingTest,
		AddToTailTest,
		AddToNonTailTest,
		AddOnUnknownParentTest,
		StatusOrProcessingPreviouslyAcceptedTest,
		StatusOrProcessingPreviouslyRejectedTest,
		StatusOrProcessingUnissuedTest,
		StatusOrProcessingIssuedTest,
		RecordPollAcceptSingleBlockTest,
		RecordPollAcceptAndRejectTest,
		RecordPollSplitVoteNoChangeTest,
		RecordPollWhenFinalizedTest,
		RecordPollRejectTransitivelyTest,
		RecordPollTransitivelyResetConfidenceTest,
		RecordPollInvalidVoteTest,
		RecordPollTransitiveVotingTest,
		RecordPollDivergedVotingWithNoConflictingBitTest,
		RecordPollChangePreferredChainTest,
		LastAcceptedTest,
		MetricsProcessingErrorTest,
		MetricsAcceptedErrorTest,
		MetricsRejectedErrorTest,
		ErrorOnAcceptTest,
		ErrorOnRejectSiblingTest,
		ErrorOnTransitiveRejectionTest,
		RandomizedConsistencyTest,
		ErrorOnAddDecidedBlockTest,
		RecordPollWithDefaultParameters,
		RecordPollRegressionCalculateInDegreeIndegreeCalculation,
	}

	errTest = errors.New("non-nil error")
)

// Execute all tests against a consensus implementation
func runConsensusTests(t *testing.T, factory Factory) {
	for _, test := range testFuncs {
		t.Run(getTestName(test), func(tt *testing.T) {
			test(tt, factory)
		})
	}
}

func getTestName(i interface{}) string {
	return strings.Split(path.Base(runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()), ".")[1]
}

// Make sure that initialize sets the state correctly
func InitializeTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	require.Equal(chaintest.GenesisID, sm.Preference())
	require.Zero(sm.NumProcessing())
}

// Make sure that the number of processing blocks is tracked correctly
func NumProcessingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)

	require.Zero(sm.NumProcessing())

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(block))
	require.Equal(1, sm.NumProcessing())

	votes := bag.Of(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Zero(sm.NumProcessing())
}

// Make sure that adding a block to the tail updates the preference
func AddToTailTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(block))
	require.Equal(block.ID(), sm.Preference())
	require.True(sm.IsPreferred(block.ID()))

	pref, ok := sm.PreferenceAtHeight(block.Height())
	require.True(ok)
	require.Equal(block.ID(), pref)
}

// Make sure that adding a block not to the tail doesn't change the preference
func AddToNonTailTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	firstBlock := chaintest.BuildChild(chaintest.Genesis)
	secondBlock := chaintest.BuildChild(chaintest.Genesis)

	// Adding to the previous preference will update the preference
	require.NoError(sm.Add(firstBlock))
	require.Equal(firstBlock.IDV, sm.Preference())

	// Adding to something other than the previous preference won't update the
	// preference
	require.NoError(sm.Add(secondBlock))
	require.Equal(firstBlock.IDV, sm.Preference())
}

// Make sure that adding a block that is detached from the rest of the tree
// returns an error
func AddOnUnknownParentTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := &chaintest.Block{
		Decidable: consensustest.Decidable{
			IDV:    ids.GenerateTestID(),
			Status: consensustest.Undecided,
		},
		ParentV: ids.GenerateTestID(),
		HeightV: chaintest.GenesisHeight + 2,
	}

	// Adding a block with an unknown parent should error.
	err := sm.Add(block)
	require.ErrorIs(err, errUnknownParentBlock)
}

func StatusOrProcessingPreviouslyAcceptedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	require.Equal(consensustest.Accepted, chaintest.Genesis.Status)
	require.False(sm.Processing(chaintest.Genesis.ID()))
	require.True(sm.IsPreferred(chaintest.Genesis.ID()))

	pref, ok := sm.PreferenceAtHeight(chaintest.Genesis.Height())
	require.True(ok)
	require.Equal(chaintest.Genesis.ID(), pref)
}

func StatusOrProcessingPreviouslyRejectedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)
	require.NoError(block.Reject(context.Background()))

	require.Equal(consensustest.Rejected, block.Status)
	require.False(sm.Processing(block.ID()))
	require.False(sm.IsPreferred(block.ID()))

	_, ok := sm.PreferenceAtHeight(block.Height())
	require.False(ok)
}

func StatusOrProcessingUnissuedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)

	require.Equal(consensustest.Undecided, block.Status)
	require.False(sm.Processing(block.ID()))
	require.False(sm.IsPreferred(block.ID()))

	_, ok := sm.PreferenceAtHeight(block.Height())
	require.False(ok)
}

func StatusOrProcessingIssuedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)

	require.NoError(sm.Add(block))
	require.Equal(consensustest.Undecided, block.Status)
	require.True(sm.Processing(block.ID()))
	require.True(sm.IsPreferred(block.ID()))

	pref, ok := sm.PreferenceAtHeight(block.Height())
	require.True(ok)
	require.Equal(block.ID(), pref)
}

func RecordPollAcceptSingleBlockTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)

	require.NoError(sm.Add(block))

	votes := bag.Of(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(block.ID(), sm.Preference())
	require.Equal(1, sm.NumProcessing())
	require.Equal(consensustest.Undecided, block.Status)

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(block.ID(), sm.Preference())
	require.Zero(sm.NumProcessing())
	require.Equal(consensustest.Accepted, block.Status)
}

func RecordPollAcceptAndRejectTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	firstBlock := chaintest.BuildChild(chaintest.Genesis)
	secondBlock := chaintest.BuildChild(chaintest.Genesis)

	require.NoError(sm.Add(firstBlock))
	require.NoError(sm.Add(secondBlock))

	votes := bag.Of(firstBlock.ID())

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.Equal(2, sm.NumProcessing())
	require.Equal(consensustest.Undecided, firstBlock.Status)
	require.Equal(consensustest.Undecided, secondBlock.Status)

	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.Zero(sm.NumProcessing())
	require.Equal(consensustest.Accepted, firstBlock.Status)
	require.Equal(consensustest.Rejected, secondBlock.Status)
}

func RecordPollSplitVoteNoChangeTest(t *testing.T, factory Factory) {
	require := require.New(t)
	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	registerer := prometheus.NewRegistry()
	ctx.Registerer = registerer

	params := sampling.Parameters{
		K:                     2,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	firstBlock := chaintest.BuildChild(chaintest.Genesis)
	secondBlock := chaintest.BuildChild(chaintest.Genesis)
	// Ensure that the blocks have at least one bit as a common prefix
	for firstBlock.IDV.Bit(0) != secondBlock.IDV.Bit(0) {
		secondBlock = chaintest.BuildChild(chaintest.Genesis)
	}

	require.NoError(sm.Add(firstBlock))
	require.NoError(sm.Add(secondBlock))

	votes := bag.Of(firstBlock.ID(), secondBlock.ID())

	// The first poll will accept shared bits
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.Equal(2, sm.NumProcessing())

	metrics := gatherCounterGauge(t, registerer)
	require.Zero(metrics["polls_failed"])
	require.InDelta(float64(1), metrics["polls_successful"], 0)

	// The second poll will do nothing
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(firstBlock.ID(), sm.Preference())
	require.Equal(2, sm.NumProcessing())

	metrics = gatherCounterGauge(t, registerer)
	require.InDelta(float64(1), metrics["polls_failed"], 0)
	require.InDelta(float64(1), metrics["polls_successful"], 0)
}

func RecordPollWhenFinalizedTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	votes := bag.Of(chaintest.GenesisID)
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Zero(sm.NumProcessing())
	require.Equal(chaintest.GenesisID, sm.Preference())
}

func RecordPollRejectTransitivelyTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(chaintest.Genesis)
	block2 := chaintest.BuildChild(block1)

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))
	require.NoError(sm.Add(block2))

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//     |
	//     2
	// Tail = 0

	votes := bag.Of(block0.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes))

	// Current graph structure:
	// 0
	// Tail = 0

	require.Zero(sm.NumProcessing())
	require.Equal(block0.ID(), sm.Preference())
	require.Equal(consensustest.Accepted, block0.Status)
	require.Equal(consensustest.Rejected, block1.Status)
	require.Equal(consensustest.Rejected, block2.Status)
}

func RecordPollTransitivelyResetConfidenceTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(chaintest.Genesis)
	block2 := chaintest.BuildChild(block1)
	block3 := chaintest.BuildChild(block1)

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))
	require.NoError(sm.Add(block2))
	require.NoError(sm.Add(block3))

	// Current graph structure:
	//   G
	//  / \
	// 0   1
	//    / \
	//   2   3

	votesFor2 := bag.Of(block2.ID())
	require.NoError(sm.RecordPoll(context.Background(), votesFor2))
	require.Equal(4, sm.NumProcessing())
	require.Equal(block2.ID(), sm.Preference())

	emptyVotes := bag.Bag[ids.ID]{}
	require.NoError(sm.RecordPoll(context.Background(), emptyVotes))
	require.Equal(4, sm.NumProcessing())
	require.Equal(block2.ID(), sm.Preference())

	require.NoError(sm.RecordPoll(context.Background(), votesFor2))
	require.Equal(4, sm.NumProcessing())
	require.Equal(block2.ID(), sm.Preference())

	votesFor3 := bag.Of(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votesFor3))
	require.Equal(2, sm.NumProcessing())
	require.Equal(block3.ID(), sm.Preference())

	require.NoError(sm.RecordPoll(context.Background(), votesFor3))
	require.Zero(sm.NumProcessing())
	require.Equal(block3.ID(), sm.Preference())
	require.Equal(consensustest.Rejected, block0.Status)
	require.Equal(consensustest.Accepted, block1.Status)
	require.Equal(consensustest.Rejected, block2.Status)
	require.Equal(consensustest.Accepted, block3.Status)
}

func RecordPollInvalidVoteTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)
	unknownBlockID := ids.GenerateTestID()

	require.NoError(sm.Add(block))

	validVotes := bag.Of(block.ID())
	require.NoError(sm.RecordPoll(context.Background(), validVotes))

	invalidVotes := bag.Of(unknownBlockID)
	require.NoError(sm.RecordPoll(context.Background(), invalidVotes))
	require.NoError(sm.RecordPoll(context.Background(), validVotes))
	require.Equal(1, sm.NumProcessing())
	require.Equal(block.ID(), sm.Preference())
}

func RecordPollTransitiveVotingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     3,
		AlphaPreference:       3,
		AlphaConfidence:       3,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(block0)
	block2 := chaintest.BuildChild(block1)
	block3 := chaintest.BuildChild(block0)
	block4 := chaintest.BuildChild(block3)

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))
	require.NoError(sm.Add(block2))
	require.NoError(sm.Add(block3))
	require.NoError(sm.Add(block4))

	// Current graph structure:
	//   G
	//   |
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	votes0_2_4 := bag.Of(block0.ID(), block2.ID(), block4.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes0_2_4))

	// Current graph structure:
	//   0
	//  / \
	// 1   3
	// |   |
	// 2   4
	// Tail = 2

	require.Equal(4, sm.NumProcessing())
	require.Equal(block2.ID(), sm.Preference())
	require.Equal(consensustest.Accepted, block0.Status)
	require.Equal(consensustest.Undecided, block1.Status)
	require.Equal(consensustest.Undecided, block2.Status)
	require.Equal(consensustest.Undecided, block3.Status)
	require.Equal(consensustest.Undecided, block4.Status)

	dep2_2_2 := bag.Of(block2.ID(), block2.ID(), block2.ID())
	require.NoError(sm.RecordPoll(context.Background(), dep2_2_2))

	// Current graph structure:
	//   2
	// Tail = 2

	require.Zero(sm.NumProcessing())
	require.Equal(block2.ID(), sm.Preference())
	require.Equal(consensustest.Accepted, block0.Status)
	require.Equal(consensustest.Accepted, block1.Status)
	require.Equal(consensustest.Accepted, block2.Status)
	require.Equal(consensustest.Rejected, block3.Status)
	require.Equal(consensustest.Rejected, block4.Status)
}

func RecordPollDivergedVotingWithNoConflictingBitTest(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := &chaintest.Block{
		Decidable: consensustest.Decidable{
			IDV:    ids.ID{0x06}, // 0110
			Status: consensustest.Undecided,
		},
		ParentV: chaintest.GenesisID,
		HeightV: chaintest.GenesisHeight + 1,
	}
	block1 := &chaintest.Block{
		Decidable: consensustest.Decidable{
			IDV:    ids.ID{0x08}, // 0001
			Status: consensustest.Undecided,
		},
		ParentV: chaintest.GenesisID,
		HeightV: chaintest.GenesisHeight + 1,
	}
	block2 := &chaintest.Block{
		Decidable: consensustest.Decidable{
			IDV:    ids.ID{0x01}, // 1000
			Status: consensustest.Undecided,
		},
		ParentV: chaintest.GenesisID,
		HeightV: chaintest.GenesisHeight + 1,
	}
	block3 := chaintest.BuildChild(block2)

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))

	// When voting for [block0], we end up finalizing the first bit as 0. The
	// second bit is contested as either 0 or 1. For when the second bit is 1,
	// the following bits have been decided to follow the 254 remaining bits of
	// [block0].
	votes0 := bag.Of(block0.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes0))

	// Although we are adding in [block2] here - the underlying confidence
	// instance has already decided it is rejected. Linear doesn't actually
	// know that though, because that is an implementation detail of the
	// Confidence trie that is used.
	require.NoError(sm.Add(block2))

	// Because [block2] is effectively rejected, [block3] is also effectively
	// rejected.
	require.NoError(sm.Add(block3))

	require.Equal(block0.ID(), sm.Preference())
	require.Equal(consensustest.Undecided, block0.Status, "should not be decided yet")
	require.Equal(consensustest.Undecided, block1.Status, "should not be decided yet")
	require.Equal(consensustest.Undecided, block2.Status, "should not be decided yet")
	require.Equal(consensustest.Undecided, block3.Status, "should not be decided yet")

	// Current graph structure:
	//       G
	//     /   \
	//    *     |
	//   / \    |
	//  0   1   2
	//          |
	//          3
	// Tail = 0

	// Transitively votes for [block2] by voting for its child [block3]. Because
	// [block2] doesn't share any processing bits with [block0] or [block1], the
	// votes are over only rejected bits. Therefore, the votes for [block2] are
	// dropped. Although the votes for [block3] are still applied, [block3] will
	// only be marked as accepted after [block2] is marked as accepted; which
	// will never happen.
	votes3 := bag.Of(block3.ID())
	require.NoError(sm.RecordPoll(context.Background(), votes3))

	require.Equal(4, sm.NumProcessing())
	require.Equal(consensustest.Undecided, block0.Status)
	require.Equal(consensustest.Undecided, block1.Status)
	require.Equal(consensustest.Undecided, block2.Status)
	require.Equal(consensustest.Undecided, block3.Status)
}

func RecordPollChangePreferredChainTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  10,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	a1Block := chaintest.BuildChild(chaintest.Genesis)
	b1Block := chaintest.BuildChild(chaintest.Genesis)
	a2Block := chaintest.BuildChild(a1Block)
	b2Block := chaintest.BuildChild(b1Block)

	require.NoError(sm.Add(a1Block))
	require.NoError(sm.Add(a2Block))
	require.NoError(sm.Add(b1Block))
	require.NoError(sm.Add(b2Block))

	require.Equal(a2Block.ID(), sm.Preference())

	require.True(sm.IsPreferred(a1Block.ID()))
	require.True(sm.IsPreferred(a2Block.ID()))
	require.False(sm.IsPreferred(b1Block.ID()))
	require.False(sm.IsPreferred(b2Block.ID()))

	pref, ok := sm.PreferenceAtHeight(a1Block.Height())
	require.True(ok)
	require.Equal(a1Block.ID(), pref)

	pref, ok = sm.PreferenceAtHeight(a2Block.Height())
	require.True(ok)
	require.Equal(a2Block.ID(), pref)

	b2Votes := bag.Of(b2Block.ID())
	require.NoError(sm.RecordPoll(context.Background(), b2Votes))

	require.Equal(b2Block.ID(), sm.Preference())
	require.False(sm.IsPreferred(a1Block.ID()))
	require.False(sm.IsPreferred(a2Block.ID()))
	require.True(sm.IsPreferred(b1Block.ID()))
	require.True(sm.IsPreferred(b2Block.ID()))

	pref, ok = sm.PreferenceAtHeight(b1Block.Height())
	require.True(ok)
	require.Equal(b1Block.ID(), pref)

	pref, ok = sm.PreferenceAtHeight(b2Block.Height())
	require.True(ok)
	require.Equal(b2Block.ID(), pref)

	a1Votes := bag.Of(a1Block.ID())
	require.NoError(sm.RecordPoll(context.Background(), a1Votes))
	require.NoError(sm.RecordPoll(context.Background(), a1Votes))

	require.Equal(a2Block.ID(), sm.Preference())
	require.True(sm.IsPreferred(a1Block.ID()))
	require.True(sm.IsPreferred(a2Block.ID()))
	require.False(sm.IsPreferred(b1Block.ID()))
	require.False(sm.IsPreferred(b2Block.ID()))

	pref, ok = sm.PreferenceAtHeight(a1Block.Height())
	require.True(ok)
	require.Equal(a1Block.ID(), pref)

	pref, ok = sm.PreferenceAtHeight(a2Block.Height())
	require.True(ok)
	require.Equal(a2Block.ID(), pref)
}

func LastAcceptedTest(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  2,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(block0)
	block2 := chaintest.BuildChild(block1)
	block1Conflict := chaintest.BuildChild(block0)

	lastAcceptedID, lastAcceptedHeight := sm.LastAccepted()
	require.Equal(chaintest.GenesisID, lastAcceptedID)
	require.Equal(chaintest.GenesisHeight, lastAcceptedHeight)

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))
	require.NoError(sm.Add(block1Conflict))
	require.NoError(sm.Add(block2))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(chaintest.GenesisID, lastAcceptedID)
	require.Equal(chaintest.GenesisHeight, lastAcceptedHeight)

	require.NoError(sm.RecordPoll(context.Background(), bag.Of(block0.IDV)))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(chaintest.GenesisID, lastAcceptedID)
	require.Equal(chaintest.GenesisHeight, lastAcceptedHeight)

	require.NoError(sm.RecordPoll(context.Background(), bag.Of(block1.IDV)))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(block0.IDV, lastAcceptedID)
	require.Equal(block0.HeightV, lastAcceptedHeight)

	require.NoError(sm.RecordPoll(context.Background(), bag.Of(block1.IDV)))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(block1.IDV, lastAcceptedID)
	require.Equal(block1.HeightV, lastAcceptedHeight)

	require.NoError(sm.RecordPoll(context.Background(), bag.Of(block2.IDV)))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(block1.IDV, lastAcceptedID)
	require.Equal(block1.HeightV, lastAcceptedHeight)

	require.NoError(sm.RecordPoll(context.Background(), bag.Of(block2.IDV)))

	lastAcceptedID, lastAcceptedHeight = sm.LastAccepted()
	require.Equal(block2.IDV, lastAcceptedID)
	require.Equal(block2.HeightV, lastAcceptedHeight)
}

func MetricsProcessingErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numProcessing := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "blks_processing",
	})

	require.NoError(ctx.Registerer.Register(numProcessing))

	err := sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func MetricsAcceptedErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numAccepted := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "blks_accepted_count",
	})

	require.NoError(ctx.Registerer.Register(numAccepted))

	err := sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func MetricsRejectedErrorTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	numRejected := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "blks_rejected_count",
	})

	require.NoError(ctx.Registerer.Register(numRejected))

	err := sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	)
	require.Error(err) //nolint:forbidigo // error is not exported https://github.com/prometheus/client_golang/blob/main/prometheus/registry.go#L315
}

func ErrorOnAcceptTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block := chaintest.BuildChild(chaintest.Genesis)
	block.AcceptV = errTest

	require.NoError(sm.Add(block))

	votes := bag.Of(block.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func ErrorOnRejectSiblingTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(chaintest.Genesis)
	block1.RejectV = errTest

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))

	votes := bag.Of(block0.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func ErrorOnTransitiveRejectionTest(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	block0 := chaintest.BuildChild(chaintest.Genesis)
	block1 := chaintest.BuildChild(chaintest.Genesis)
	block2 := chaintest.BuildChild(block1)
	block2.RejectV = errTest

	require.NoError(sm.Add(block0))
	require.NoError(sm.Add(block1))
	require.NoError(sm.Add(block2))

	votes := bag.Of(block0.ID())
	err := sm.RecordPoll(context.Background(), votes)
	require.ErrorIs(err, errTest)
}

func RandomizedConsistencyTest(t *testing.T, factory Factory) {
	require := require.New(t)

	var (
		numColors = 50
		numNodes  = 100
		params    = sampling.Parameters{
			K:                     20,
			AlphaPreference:       15,
			AlphaConfidence:       15,
			Beta:                  20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		}
		seed   uint64 = 0
		source        = prng.NewMT19937()
	)

	source.Seed(seed)

	n := NewNetwork(params, numColors, source)

	for i := 0; i < numNodes; i++ {
		require.NoError(n.AddNode(t, factory.New()))
	}

	for !n.Finalized() {
		require.NoError(n.Round())
	}

	require.True(n.Agreement())
}

func ErrorOnAddDecidedBlockTest(t *testing.T, factory Factory) {
	sm := factory.New()
	require := require.New(t)

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     1,
		AlphaPreference:       1,
		AlphaConfidence:       1,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	// Initialize with a different block ID than Genesis
	// This way when we add Genesis, its parent won't be found
	differentID := ids.GenerateTestID()
	require.NoError(sm.Initialize(
		ctx,
		params,
		differentID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	err := sm.Add(chaintest.Genesis)
	require.ErrorIs(err, errUnknownParentBlock)
}

func gatherCounterGauge(t *testing.T, reg prometheus.Gatherer) map[string]float64 {
	ms, err := reg.Gather()
	require.NoError(t, err)
	mss := make(map[string]float64)
	for _, mf := range ms {
		name := mf.GetName()
		for _, m := range mf.GetMetric() {
			cnt := m.GetCounter()
			if cnt != nil {
				mss[name] = cnt.GetValue()
				break
			}
			gg := m.GetGauge()
			if gg != nil {
				mss[name] = gg.GetValue()
				break
			}
		}
	}
	return mss
}

// You can run this test with "go test -v -run TestTopological/RecordPollWithDefaultParameters"
func RecordPollWithDefaultParameters(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.DefaultParameters
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	// "blk1" and "blk2" are in conflict
	blk1 := chaintest.BuildChild(chaintest.Genesis)
	blk2 := chaintest.BuildChild(chaintest.Genesis)

	require.NoError(sm.Add(blk1))
	require.NoError(sm.Add(blk2))

	votes := bag.Bag[ids.ID]{}
	votes.AddCount(blk1.ID(), params.AlphaConfidence)
	// Require beta rounds to finalize
	for i := 0; i < params.Beta; i++ {
		// should not finalize with less than beta rounds
		require.Equal(2, sm.NumProcessing())
		require.NoError(sm.RecordPoll(context.Background(), votes))
	}
	require.Zero(sm.NumProcessing())
}

// If a block that was voted for received additional votes from another block,
// the indegree of the topological sort should not traverse into the parent
// node.
func RecordPollRegressionCalculateInDegreeIndegreeCalculation(t *testing.T, factory Factory) {
	require := require.New(t)

	sm := factory.New()

	consensusCtx := consensustest.Context(t, consensustest.CChainID)
	ctx := consensustest.ConsensusContext(consensusCtx)
	params := sampling.Parameters{
		K:                     3,
		AlphaPreference:       2,
		AlphaConfidence:       2,
		Beta:                  1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}
	require.NoError(sm.Initialize(
		ctx,
		params,
		chaintest.GenesisID,
		chaintest.GenesisHeight,
		chaintest.GenesisTimestamp,
	))

	blk1 := chaintest.BuildChild(chaintest.Genesis)
	blk2 := chaintest.BuildChild(blk1)
	blk3 := chaintest.BuildChild(blk2)

	require.NoError(sm.Add(blk1))
	require.NoError(sm.Add(blk2))
	require.NoError(sm.Add(blk3))

	votes := bag.Bag[ids.ID]{}
	votes.AddCount(blk2.ID(), 1)
	votes.AddCount(blk3.ID(), 2)
	require.NoError(sm.RecordPoll(context.Background(), votes))
	require.Equal(consensustest.Accepted, blk1.Status)
	require.Equal(consensustest.Accepted, blk2.Status)
	require.Equal(consensustest.Accepted, blk3.Status)
}

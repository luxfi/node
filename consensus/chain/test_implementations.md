# Snowman Consensus Test Implementations

This document provides detailed implementation guidance for the TODO tests based on the avalanchego consensus test file.

## 1. AddOnUnknownParentTest

**Purpose**: Tests that adding a block with an unknown parent returns an error.

**Implementation Details**:
- Create a block with a parent ID that doesn't exist in the consensus
- The parent ID should be a randomly generated ID that's not Genesis
- When adding this block, it should return `errUnknownParentBlock`

```go
func AddOnUnknownParentTest(t *testing.T, factory Factory) {
    require := require.New(t)
    sm := factory.New()
    params := factory.Default()
    
    ctx := context.WithValue(context.Background(), "consensus", &consensus.Context{
        ChainID: consensustest.CChainID,
    })
    
    require.NoError(sm.Initialize(ctx, params, chaintest.GenesisID.String(), chaintest.GenesisTimestamp, chaintest.GenesisTimestamp))
    
    // Create a block with unknown parent
    block := &chaintest.Block{
        IDV:       ids.GenerateTestID(),
        ParentV:   ids.GenerateTestID(), // Unknown parent
        HeightV:   chaintest.GenesisHeight + 2,
        StatusV:   choices.Processing,
        TimestampV: chaintest.GenesisTimestamp + 1,
    }
    
    // Adding a block with unknown parent should error
    err := sm.Add(context.Background(), block)
    require.Error(err)
    // Check for specific error type if available
}
```

## 2. StatusOrProcessingPreviouslyRejectedTest

**Purpose**: Tests the status/processing state of a previously rejected block.

**Implementation Details**:
- Create and add a block
- Manually reject it using block.Reject()
- Verify block status is Rejected
- Verify Processing() returns false
- Verify IsPreferred() returns false
- Verify PreferenceAtHeight() returns false for that height

```go
func StatusOrProcessingPreviouslyRejectedTest(t *testing.T, factory Factory) {
    require := require.New(t)
    sm := factory.New()
    // Initialize...
    
    block := chaintest.BuildChild(chaintest.Genesis)
    require.NoError(block.Reject(context.Background()))
    
    require.Equal(choices.Rejected, block.StatusV)
    require.False(sm.Processing(block.IDV))
    require.False(sm.IsPreferred(block))
}
```

## 3. StatusOrProcessingUnissuedTest

**Purpose**: Tests status of a block that hasn't been added to consensus yet.

**Implementation Details**:
- Create a block but don't add it
- Status should be Unknown/Processing
- Processing() should return false
- IsPreferred() should return false

## 4. StatusOrProcessingIssuedTest

**Purpose**: Tests status of a block that has been added but not decided.

**Implementation Details**:
- Add a block to consensus
- Status should be Processing
- Processing() should return true
- IsPreferred() should return true (if it extends the preferred chain)

## 5. RecordPollAcceptAndRejectTest

**Purpose**: Tests that voting accepts one block and rejects its competitor.

**Implementation Details**:
- Add two conflicting blocks (same parent)
- Vote for one block with sufficient votes (Beta rounds)
- Verify the voted block is Accepted
- Verify the other block is Rejected
- NumProcessing should go to 0

## 6. RecordPollSplitVoteNoChangeTest

**Purpose**: Tests split voting that doesn't reach consensus.

**Implementation Details**:
- Parameters: K=2, AlphaPreference=2, AlphaConfidence=2
- Add two conflicting blocks
- Vote with split votes (1 for each)
- No block should be finalized
- Test metrics for failed polls

## 7. RecordPollWhenFinalizedTest

**Purpose**: Tests voting when consensus is already finalized.

**Implementation Details**:
- Vote for Genesis (already accepted)
- Should handle gracefully with no changes
- NumProcessing should remain 0

## 8. RecordPollRejectTransitivelyTest

**Purpose**: Tests that accepting a block rejects conflicting branches.

**Implementation Details**:
- Structure: Genesis -> {block0, block1 -> block2}
- Vote to accept block0
- block1 and block2 should be transitively rejected

## 9. RecordPollTransitivelyResetConfidenceTest

**Purpose**: Tests confidence reset when switching preferred chains.

**Implementation Details**:
- Complex voting scenario with preference changes
- Tests Beta parameter (confidence threshold)
- Demonstrates preference can switch with enough votes

## 10. RecordPollInvalidVoteTest

**Purpose**: Tests handling of votes for unknown blocks.

**Implementation Details**:
- Vote includes unknown block IDs
- Should ignore invalid votes
- Valid votes should still be processed

## 11. RecordPollTransitiveVotingTest

**Purpose**: Tests transitive voting effects through a chain.

**Implementation Details**:
- Deep chain structure with branches
- Voting affects entire chains transitively
- K=3, AlphaPreference=3 parameters

## 12. RecordPollDivergedVotingWithNoConflictingBitTest

**Purpose**: Tests edge case in snowball bit voting.

**Implementation Details**:
- Uses specific block IDs to test bit-level voting
- Complex scenario with non-overlapping bits
- Tests internal snowball trie implementation

## 13. RecordPollChangePreferredChainTest

**Purpose**: Tests preference changes between competing chains.

**Implementation Details**:
- Two chains: a1->a2 and b1->b2
- Initial preference for 'a' chain
- Vote for 'b' chain to switch preference
- Vote back to 'a' chain
- Tests IsPreferred() and PreferenceAtHeight()

## 14. LastAcceptedTest

**Purpose**: Tests LastAccepted tracking through block acceptance.

**Implementation Details**:
- Build chain: Genesis -> block0 -> block1 -> block2
- Track LastAccepted after each poll
- Verify height and ID updates correctly

## 15. MetricsProcessingErrorTest

**Purpose**: Tests metric registration conflicts.

**Implementation Details**:
- Pre-register "blks_processing" metric
- Initialize should fail with registration error
- Tests proper error handling

## 16. MetricsAcceptedErrorTest

**Purpose**: Similar to above but for "blks_accepted_count" metric.

## 17. MetricsRejectedErrorTest

**Purpose**: Similar to above but for "blks_rejected_count" metric.

## 18. ErrorOnAcceptTest

**Purpose**: Tests error handling when Accept() fails.

**Implementation Details**:
- Create block with AcceptV = errTest
- RecordPoll should propagate the error

## 19. ErrorOnRejectSiblingTest

**Purpose**: Tests error handling when rejecting sibling fails.

**Implementation Details**:
- Two competing blocks
- One has RejectV = errTest
- Accepting the other should propagate reject error

## 20. ErrorOnTransitiveRejectionTest

**Purpose**: Tests error propagation in transitive rejection.

**Implementation Details**:
- Chain with descendant having RejectV = errTest
- Error should propagate when rejecting transitively

## 21. RandomizedConsistencyTest

**Purpose**: Tests consensus consistency with random voting.

**Implementation Details**:
- 50 colors (block choices)
- 100 nodes
- Random voting until consensus
- Verify all nodes agree

## 22. ErrorOnAddDecidedBlockTest

**Purpose**: Tests adding an already decided block.

**Implementation Details**:
- Try to add Genesis (already accepted)
- Should return error

## 23. RecordPollWithDefaultParameters

**Purpose**: Tests with production parameters.

**Implementation Details**:
- Uses snowball.DefaultParameters
- Two conflicting blocks
- Vote with AlphaConfidence votes
- Requires Beta rounds to finalize

## 24. RecordPollRegressionCalculateInDegreeIndegreeCalculation

**Purpose**: Tests specific edge case in vote counting.

**Implementation Details**:
- Chain: Genesis -> blk1 -> blk2 -> blk3
- Vote: 1 for blk2, 2 for blk3
- Tests vote aggregation doesn't double-count
// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// dispatch_test.go — a block is executed by whichever visitor arm it reaches,
// so the arm it reaches IS its meaning. Nothing downstream re-checks the kind:
// a standard block that arrives at CommitBlock is accepted as an empty
// decision, and every transaction it carries is dropped from state while the
// block itself is final. The wire kind and the arm must agree, whether the
// block was just built or just parsed off the wire.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// armRecorder records which arm was reached, and nothing else. Two arms firing
// for one block, or none, is as wrong as the wrong one firing.
type armRecorder struct{ arms []string }

func (r *armRecorder) AbortBlock(*AbortBlock) error       { r.arms = append(r.arms, "abort"); return nil }
func (r *armRecorder) CommitBlock(*CommitBlock) error     { r.arms = append(r.arms, "commit"); return nil }
func (r *armRecorder) ProposalBlock(*ProposalBlock) error { r.arms = append(r.arms, "proposal"); return nil }
func (r *armRecorder) StandardBlock(*StandardBlock) error { r.arms = append(r.arms, "standard"); return nil }

func blockOfEachKind(t *testing.T) map[string]Block {
	t.Helper()

	parentID := ids.GenerateTestID()
	at := time.Unix(0, 0)

	abort, err := NewAbortBlock(at, parentID, 1)
	require.NoError(t, err)
	commit, err := NewCommitBlock(at, parentID, 1)
	require.NoError(t, err)
	standard, err := NewStandardBlock(at, parentID, 1, []*txs.Tx{decisionTx(t)})
	require.NoError(t, err)

	proposalTx := &txs.Tx{Unsigned: txs.NewRewardValidatorTx(ids.GenerateTestID())}
	require.NoError(t, proposalTx.Initialize())
	proposal, err := NewProposalBlock(at, parentID, 1, proposalTx, []*txs.Tx{decisionTx(t)})
	require.NoError(t, err)

	return map[string]Block{
		"abort":    abort,
		"commit":   commit,
		"standard": standard,
		"proposal": proposal,
	}
}

// Every block reaches its own arm and only its own — as built, and again after
// a trip through the wire.
func TestBlockReachesItsOwnVisitorArm(t *testing.T) {
	for kind, blk := range blockOfEachKind(t) {
		t.Run(kind, func(t *testing.T) {
			require := require.New(t)

			built := &armRecorder{}
			require.NoError(blk.Visit(built))
			require.Equal([]string{kind}, built.arms,
				"the block was executed as the wrong kind of block")

			parsed, err := Parse(blk.Bytes())
			require.NoError(err)

			round := &armRecorder{}
			require.NoError(parsed.Visit(round))
			require.Equal([]string{kind}, round.arms,
				"the block changed meaning on its way through the wire")
		})
	}
}

// The two kinds that decide a proposal carry no transactions of their own. If
// either started reporting some, they would be executed twice — once in the
// proposal that produced them and once again here.
func TestDecidingBlocksCarryNoTxs(t *testing.T) {
	blocks := blockOfEachKind(t)

	for _, kind := range []string{"abort", "commit"} {
		t.Run(kind, func(t *testing.T) {
			require.Empty(t, blocks[kind].DecisionTxs())

			parsed, err := Parse(blocks[kind].Bytes())
			require.NoError(t, err)
			require.Empty(t, parsed.DecisionTxs())
		})
	}
}

// A block's identity is its bytes. Two blocks that differ in any field a
// validator reads must not share an ID, or one silently stands in for the
// other in every cache, index and vote that keys on it.
func TestBlocksDifferingInAnyFieldDifferInID(t *testing.T) {
	require := require.New(t)

	parentID := ids.GenerateTestID()
	at := time.Unix(1000, 0)

	base, err := NewStandardBlock(at, parentID, 1, []*txs.Tx{decisionTx(t)})
	require.NoError(err)

	otherParent, err := NewStandardBlock(at, ids.GenerateTestID(), 1, base.DecisionTxs())
	require.NoError(err)
	require.NotEqual(base.ID(), otherParent.ID(), "parent is not part of the identity")

	otherHeight, err := NewStandardBlock(at, parentID, 2, base.DecisionTxs())
	require.NoError(err)
	require.NotEqual(base.ID(), otherHeight.ID(), "height is not part of the identity")

	otherTime, err := NewStandardBlock(at.Add(time.Second), parentID, 1, base.DecisionTxs())
	require.NoError(err)
	require.NotEqual(base.ID(), otherTime.ID(), "timestamp is not part of the identity")

	// decisionTx is a fixed fixture, so a second one is byte-identical to the
	// first; carrying two of them is what makes this block genuinely differ.
	otherTxs, err := NewStandardBlock(at, parentID, 1, []*txs.Tx{decisionTx(t), decisionTx(t)})
	require.NoError(err)
	require.NotEqual(base.ID(), otherTxs.ID(), "the transactions are not part of the identity")

	// An abort and a commit at the same position are different decisions.
	abort, err := NewAbortBlock(at, parentID, 1)
	require.NoError(err)
	commit, err := NewCommitBlock(at, parentID, 1)
	require.NoError(err)
	require.NotEqual(abort.ID(), commit.ID(),
		"an abort and a commit over the same proposal share an ID, so one stands in for the other")
}

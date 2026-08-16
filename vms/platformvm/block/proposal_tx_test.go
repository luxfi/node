// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// A block's own transaction is not one it charges for.
//
// A proposal block carries two different kinds of transaction. The decision txs
// were submitted to this chain and are paid for by whoever submitted them. The
// proposal tx is emitted by the chain about itself — a staker's reward, a time
// advance — and neither fee calculator will price one.
//
// So the set a block charges for and the set it contains are different sets,
// and the proposal tx is the entire difference. Answering the first question
// with the second asks what a chain-emitted transaction costs, which has no
// answer: the block cannot verify, and it is rebuilt and refused again on every
// round, forever. DecisionTxs is the only list any block hands out, and the
// proposal tx is reachable only as (*ProposalBlock).Tx().

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/bench"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
)

// rewardBlock builds the block a chain produces when a staker's period has
// ended: one chain-emitted reward, and whatever was in the mempool.
func rewardBlock(t *testing.T, decisionTxs []*txs.Tx) *ProposalBlock {
	t.Helper()
	reward := &txs.Tx{Unsigned: txs.NewRewardValidatorTx(ids.GenerateTestID())}
	require.NoError(t, reward.Initialize())

	blk, err := NewProposalBlock(time.Unix(0, 0), ids.GenerateTestID(), 1, reward, decisionTxs)
	require.NoError(t, err)
	return blk
}

// TestDecisionTxsExcludesTheUnpriceableProposalTx is the failure this file
// exists for. Everything a block hands out as a list reaches the fee path, so
// everything in that list must be priceable; the proposal tx never is.
func TestDecisionTxsExcludesTheUnpriceableProposalTx(t *testing.T) {
	blk := rewardBlock(t, nil)

	// The premise, asserted rather than assumed: this transaction is one nothing
	// will price. Without it the rest of the test proves nothing.
	_, err := fee.TxComplexity(blk.Tx().Unsigned)
	require.ErrorIs(t, err, fee.ErrUnsupportedTx,
		"premise gone: the proposal tx is priceable now, so it cannot demonstrate the refusal")

	for _, tx := range blk.DecisionTxs() {
		require.NotEqual(t, blk.Tx().ID(), tx.ID(),
			"the proposal tx is in the set handed to the fee path, so verification prices a "+
				"transaction the chain emitted about itself, refuses the block, and rebuilds "+
				"and refuses it again on every round")
	}

	// And with a real one present, nothing in that set may be unpriceable.
	withDecision := rewardBlock(t, []*txs.Tx{decisionTx(t)})
	require.NotEmpty(t, withDecision.DecisionTxs())
	for i, tx := range withDecision.DecisionTxs() {
		_, err := fee.TxComplexity(tx.Unsigned)
		require.NoError(t, err,
			"tx %d in the priced set cannot be priced, so the block can never verify", i)
	}
}

// TestProposalBlockWithoutItsTxIsRefused holds up the other half: Tx() is the
// only route to the proposal tx, so every reader dereferences it directly. A
// proposal block that does not carry one is refused where it enters — built or
// parsed — rather than reaching those readers as a nil.
func TestProposalBlockWithoutItsTxIsRefused(t *testing.T) {
	_, err := NewProposalBlock(time.Unix(0, 0), ids.GenerateTestID(), 1, nil, []*txs.Tx{decisionTx(t)})
	require.ErrorIs(t, err, errNoProposalTx)

	// A tx that was never initialized carries no bytes, so the slot it writes is
	// empty and indistinguishable on the wire from one that was never written.
	// Both entrances have to weigh the slot rather than the pointer: the builder
	// can produce this shape as readily as a peer can send it, and a locally
	// built block never passes through Parse.
	_, err = NewProposalBlock(time.Unix(0, 0), ids.GenerateTestID(), 1, &txs.Tx{}, nil)
	require.ErrorIs(t, err, errNoProposalTx)

	empty, err := buildBlock(blkProposal, ids.GenerateTestID(), 1, 0, []*txs.Tx{decisionTx(t)}, emptySlotTx())
	require.NoError(t, err)
	_, err = Parse(empty)
	require.ErrorIs(t, err, errNoProposalTx)
}

// emptySlotTx is a tx the builder's own refusal cannot see: it reports bytes, so
// it passes the build check, and those bytes do not decode, so the slot Parse
// reads is empty. That is the shape only Parse can answer for.
func emptySlotTx() *txs.Tx {
	tx := &txs.Tx{}
	tx.SetBytes([]byte{0})
	return tx
}

// decisionTx returns a transaction of the kind a user submits. It must be one
// the pricer ACCEPTS: standing in a refused type here would make the priced set
// look correct while proving the opposite of what is claimed.
func decisionTx(t *testing.T) *txs.Tx {
	t.Helper()
	tx := &txs.Tx{Unsigned: bench.NewBaseTxFixture()}
	require.NoError(t, tx.Initialize())

	_, err := fee.TxComplexity(tx.Unsigned)
	require.NoError(t, err, "this stand-in is not priceable, so it cannot represent a decision tx")
	return tx
}


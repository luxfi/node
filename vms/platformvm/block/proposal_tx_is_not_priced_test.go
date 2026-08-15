// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// proposal_tx_is_not_priced_test.go — a block's own transaction is not one it charges for.
//
// A proposal block carries two different kinds of transaction. The decision txs
// were submitted to this chain and are paid for by whoever submitted them. The
// proposal tx is emitted by the chain about itself — a staker's reward, a time
// advance — and neither fee calculator will price one: the static visitor and
// the complexity visitor both refuse it.
//
// So a reader that means "everything in this block" and a reader that means
// "the transactions this block charges for" are asking different questions, and
// the proposal tx is the entire difference between them. Answering the second
// with the first asks what a chain-emitted transaction costs, which has no
// answer — the refusal surfaces as a block that cannot be verified, rebuilt
// identically and refused again on every round, forever.

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
// exists for. Everything the verifier hands to the fee path must be priceable;
// the proposal tx never is, so it must not be in that set.
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

// TestTxsStillCarriesTheWholeBlock is the other half. Txs() answers the Block
// interface's question — everything the block contains — and metrics, warp
// verification and rejection all depend on that. Narrowing it to fix the pricing
// path would silently stop those three seeing the proposal tx at all.
func TestTxsStillCarriesTheWholeBlock(t *testing.T) {
	decision := decisionTx(t)
	blk := rewardBlock(t, []*txs.Tx{decision})

	all := blk.Txs()
	require.Len(t, all, 2, "Txs must carry the decision txs AND the proposal tx")
	require.Equal(t, decision.ID(), all[0].ID(), "decision txs come first")
	require.Equal(t, blk.Tx().ID(), all[1].ID(), "the proposal tx is last")

	require.Len(t, blk.DecisionTxs(), 1, "DecisionTxs carries only what the block charges for")
	require.Equal(t, decision.ID(), blk.DecisionTxs()[0].ID())
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

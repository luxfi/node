// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/xvm/block"
	blkexecutor "github.com/luxfi/node/vms/xvm/block/executor"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	txexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
)

func u64ptr(v uint64) *uint64 { return &v }

// TestMerkleRootGateOffDefaultBlockEmpty is deliverable case (a), end to end: a
// VM with the default config (gate OFF, never sentinel) builds and accepts a
// real block whose merkle root is empty — the historical behavior, unchanged.
func TestMerkleRootGateOffDefaultBlockEmpty(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgrade.Default})
	env.vm.Lock.Unlock()

	tx := newTx(t, env.genesisBytes, env.consensusRuntime.ChainID, env.vm.parser, "LUX")
	require.NoError(env.vm.network.IssueTxFromRPC(tx))

	blkIntf, err := env.vm.BuildBlock(context.Background())
	require.NoError(err)

	require.Equal(ids.Empty, blkIntf.(*blkexecutor.Block).MerkleRoot(),
		"gate off: built block must carry an empty root")

	// And it verifies and accepts under the historical empty-root rule.
	require.NoError(blkIntf.Verify(context.Background()))
	require.NoError(blkIntf.Accept(context.Background()))
}

// TestMerkleRootGateOnAcceptsAndRejects is deliverable case (b), end to end: a
// VM with the gate ON (activation height 0) builds a real block that carries a
// non-empty xvm execution_root, that block verifies and accepts, and a sibling
// block whose root is tampered is rejected with ErrUnexpectedMerkleRoot.
func TestMerkleRootGateOnAcceptsAndRejects(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork:                       upgrade.Default,
		merkleRootActivationHeight: u64ptr(0), // activate from genesis
	})
	env.vm.Lock.Unlock()

	tx := newTx(t, env.genesisBytes, env.consensusRuntime.ChainID, env.vm.parser, "LUX")
	require.NoError(env.vm.network.IssueTxFromRPC(tx))

	blkIntf, err := env.vm.BuildBlock(context.Background())
	require.NoError(err)

	// The builder stamped the execution_root: non-empty, and equal to the
	// canonical projection over the block's parent root + post-block state. The
	// CreateAssetTx in this block produces UTXOs, so the projected UTXO family is
	// non-empty — the stamped root is the real execution_root, not a degenerate
	// (empty-family) value.
	built := blkIntf.(*blkexecutor.Block)
	root := built.MerkleRoot()
	require.NotEqual(ids.Empty, root, "gate on: built block must carry a non-empty root")

	parentID := built.Parent()
	parentBlk, err := env.vm.chainManager.GetStatelessBlock(parentID)
	require.NoError(err)

	// Recompute the canonical root over the real post-block state, mirroring how
	// block verification builds it: a state diff on the parent with the block's
	// txs executed. The stamped root must equal this canonical projection, and
	// because the diff holds the block's freshly-produced UTXOs the UTXO family
	// (and thus the root) is non-trivial.
	postState := postBlockState(t, env, parentID, built.Txs())
	wantRoot, err := blkexecutor.BlockExecutionRoot(parentBlk.MerkleRoot(), built.Txs(), postState, built.Height())
	require.NoError(err)
	require.Equal(wantRoot, root, "stamped root must equal the canonical execution_root over the post-block state")

	// The projected UTXO family is genuinely non-empty (the CreateAssetTx
	// produced outputs), so the stamped root is the real execution_root.
	postUTXOs, err := postState.UTXOs(ids.Empty, 0)
	require.NoError(err)
	require.NotEmpty(postUTXOs, "gate on: the post-block UTXO set the root commits to must be non-empty")

	// Build a sibling block identical in every way EXCEPT a deliberately wrong
	// root, parse it through the VM, and verify it: the executor recomputes the
	// expected root and rejects the mismatch.
	cm := env.vm.parser.Codec()
	wrongRoot := ids.GenerateTestID()
	require.NotEqual(wrongRoot, root)
	tampered, err := block.NewStandardBlockWithRoot(
		parentID,
		built.Height(),
		time.Unix(int64(built.Timestamp().Unix()), 0),
		wrongRoot,
		built.Txs(),
		cm,
	)
	require.NoError(err)
	tamperedParsed, err := env.vm.ParseBlock(context.Background(), tampered.Bytes())
	require.NoError(err)
	err = tamperedParsed.Verify(context.Background())
	require.ErrorIs(err, blkexecutor.ErrUnexpectedMerkleRoot)

	// The correctly-stamped block still verifies and accepts.
	require.NoError(blkIntf.Verify(context.Background()))
	require.NoError(blkIntf.Accept(context.Background()))
}

// postBlockState reconstructs the post-block state a block's execution_root is
// projected over: a state diff on [parentID] with [blkTxs] executed in order.
// It mirrors the state mutation block verification performs (vms/xvm/block/
// executor/block.go) using the same txs executor, so the projected UTXO set is
// exactly the one the verifier folds. It does not re-run semantic verification
// (validation), only the state mutation (consume inputs / produce outputs) that
// determines the occupied set the root commits to.
func postBlockState(t *testing.T, env *testEnv, parentID ids.ID, blkTxs []*txs.Tx) state.Chain {
	t.Helper()
	require := require.New(t)

	diff, err := state.NewDiff(parentID, env.vm.chainManager)
	require.NoError(err)

	for _, tx := range blkTxs {
		executor := &txexecutor.Executor{
			Codec:  env.vm.parser.Codec(),
			State:  diff,
			Tx:     tx,
			Inputs: set.NewSet[ids.ID](0),
		}
		require.NoError(tx.Unsigned.Visit(executor))
		diff.AddTx(tx)
	}
	return diff
}

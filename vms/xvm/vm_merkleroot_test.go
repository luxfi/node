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

// TestMerkleRootStampedAndVerified is the always-active end-to-end proof: a VM
// with the default config builds a real block that carries the xvm
// execution_root over the post-block state (never empty), that block verifies
// and accepts, and a sibling block whose root is tampered is rejected with
// ErrUnexpectedMerkleRoot. There is no activation gate — the execution_root is
// part of the current version, stamped and verified unconditionally.
func TestMerkleRootStampedAndVerified(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgrade.Default})
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
	require.NotEqual(ids.Empty, root, "built block must carry a non-empty execution_root")

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
	require.NotEmpty(postUTXOs, "the post-block UTXO set the root commits to must be non-empty")

	// Build a sibling block identical in every way EXCEPT a deliberately wrong
	// root, parse it through the VM, and verify it: the executor recomputes the
	// expected root and rejects the mismatch.
	wrongRoot := ids.GenerateTestID()
	require.NotEqual(wrongRoot, root)
	tampered, err := block.NewStandardBlockWithRoot(
		parentID,
		built.Height(),
		time.Unix(int64(built.Timestamp().Unix()), 0),
		wrongRoot,
		built.Txs(),
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

// TestMerkleRootEmptyRootRejected confirms the empty root is not a valid block
// root under the always-active rule: a block whose root is ids.Empty (the
// historical pre-activation shape) no longer verifies, because the executor
// recomputes the real execution_root and rejects the mismatch. This pins that
// there is no surviving empty-root path.
func TestMerkleRootEmptyRootRejected(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgrade.Default})
	env.vm.Lock.Unlock()

	tx := newTx(t, env.genesisBytes, env.consensusRuntime.ChainID, env.vm.parser, "LUX")
	require.NoError(env.vm.network.IssueTxFromRPC(tx))

	blkIntf, err := env.vm.BuildBlock(context.Background())
	require.NoError(err)
	built := blkIntf.(*blkexecutor.Block)

	// A sibling block identical to the built block but carrying an EMPTY root.
	empty, err := block.NewStandardBlock(
		built.Parent(),
		built.Height(),
		time.Unix(int64(built.Timestamp().Unix()), 0),
		built.Txs(),
	)
	require.NoError(err)
	require.Equal(ids.Empty, empty.MerkleRoot())

	emptyParsed, err := env.vm.ParseBlock(context.Background(), empty.Bytes())
	require.NoError(err)
	err = emptyParsed.Verify(context.Background())
	require.ErrorIs(err, blkexecutor.ErrUnexpectedMerkleRoot,
		"an empty root must be rejected: the real execution_root is non-empty")
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
			State:  diff,
			Tx:     tx,
			Inputs: set.NewSet[ids.ID](0),
		}
		require.NoError(tx.Unsigned.Visit(executor))
		diff.AddTx(tx)
	}
	return diff
}

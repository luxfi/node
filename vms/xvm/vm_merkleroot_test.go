// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/xvm/block"
	blkexecutor "github.com/luxfi/node/vms/xvm/block/executor"
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
	// canonical projection over the block's parent root + tx family.
	built := blkIntf.(*blkexecutor.Block)
	root := built.MerkleRoot()
	require.NotEqual(ids.Empty, root, "gate on: built block must carry a non-empty root")

	parentID := built.Parent()
	parentBlk, err := env.vm.chainManager.GetStatelessBlock(parentID)
	require.NoError(err)
	wantRoot := blkexecutor.BlockExecutionRoot(parentBlk.MerkleRoot(), built.Txs(), nil, built.Height())
	require.Equal(wantRoot, root, "stamped root must equal the canonical execution_root")

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

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/xvm/block"
	blkexecutor "github.com/luxfi/node/vms/xvm/block/executor"
	"github.com/luxfi/node/vms/xvm/block/executor/executormock"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/state/statemock"
	"github.com/luxfi/node/vms/xvm/txs"
	txexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
	"github.com/luxfi/node/vms/xvm/txs/mempool"
	"github.com/luxfi/node/vms/xvm/txs/txsmock"
	"github.com/luxfi/timer/mockable"
	chain "github.com/luxfi/vm/chain"
)

// buildOneTxBlock drives BuildBlock with a single always-valid mocked tx over a
// mocked manager/state, parent at [parentHeight] with [parentRoot]. It returns
// the *StandardBlock the builder handed to NewBlock so the test can inspect the
// stamped Root. The builder always stamps the xvm execution_root — there is no
// activation gate — so the block's Root is the canonical execution_root over the
// post-block state.
func buildOneTxBlock(
	t *testing.T,
	ctrl *gomock.Controller,
	parentRoot ids.ID,
	parentHeight uint64,
) *block.StandardBlock {
	t.Helper()
	require := require.New(t)

	preferredID := ids.GenerateTestID()
	preferredTimestamp := time.Now()

	preferredBlock := block.NewMockBlock(ctrl)
	preferredBlock.EXPECT().Height().Return(parentHeight).AnyTimes()
	preferredBlock.EXPECT().Timestamp().Return(preferredTimestamp).AnyTimes()
	preferredBlock.EXPECT().MerkleRoot().Return(parentRoot).AnyTimes()

	preferredState := statemock.NewChain(ctrl)
	preferredState.EXPECT().GetLastAccepted().Return(preferredID).AnyTimes()
	preferredState.EXPECT().GetTimestamp().Return(preferredTimestamp).AnyTimes()
	// The execution_root projection enumerates the post-block occupied UTXO set
	// through the parent chain. This unit's tx produces no UTXOs, so the parent
	// (and thus the post-block) UTXO set is empty — the UTXO family folds to the
	// empty root, leaving the tx family + parent root + height to make the
	// stamped root non-empty.
	preferredState.EXPECT().UTXOs(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	// One tx that passes semantic verification and execution and adds no inputs.
	unsignedTx := txsmock.NewUnsignedTx(ctrl)
	unsignedTx.EXPECT().Visit(gomock.Any()).Return(nil)  // semantic verification
	unsignedTx.EXPECT().Visit(gomock.Any()).DoAndReturn( // execution
		func(visitor txs.Visitor) error {
			require.IsType(&txexecutor.Executor{}, visitor)
			ex := visitor.(*txexecutor.Executor)
			if ex.Inputs == nil {
				ex.Inputs = set.NewSet[ids.ID](0)
			}
			return nil
		},
	)
	unsignedTx.EXPECT().SetBytes(gomock.Any()).AnyTimes()
	unsignedTx.EXPECT().InputIDs().Return(nil).AnyTimes()
	tx := &txs.Tx{Unsigned: unsignedTx}
	// Give the tx a real, stable ID (ID = hash of signed bytes). The
	// execution_root's tx family binds tx.ID(), so a deterministic ID lets the
	// test recompute the expected root.
	tx.SetBytes(nil, []byte{0x0A, 0x0B, 0x0C, 0x0D})

	manager := executormock.NewManager(ctrl)
	manager.EXPECT().Preferred().Return(preferredID)
	manager.EXPECT().GetStatelessBlock(preferredID).Return(preferredBlock, nil)
	// GetState(preferredID) is consulted by NewDiff and again by the
	// execution_root projection (the diff fetches its parent state to enumerate
	// the post-block UTXO set), so allow repeated calls.
	manager.EXPECT().GetState(preferredID).Return(preferredState, true).AnyTimes()
	manager.EXPECT().VerifyUniqueInputs(preferredID, gomock.Any()).Return(nil).AnyTimes()

	var built *block.StandardBlock
	manager.EXPECT().NewBlock(gomock.Any()).DoAndReturn(
		func(b *block.StandardBlock) chain.Block {
			built = b
			return nil
		},
	)

	memPool, err := mempool.New("", metric.NewRegistry())
	require.NoError(err)
	require.NoError(memPool.Add(tx))

	// The builder serializes the block natively (ZAP struct-is-wire); there is
	// no codec to inject. The test reads block.Root (stamped before serialize).
	builder := New(
		&txexecutor.Backend{
			Ctx:     context.Background(),
			Runtime: testRuntime(),
			Config:  &config.Config{},
			Log:     log.NewNoOpLogger(),
		},
		manager,
		&mockable.Clock{},
		memPool,
	)

	_, err = builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NotNil(built)
	return built
}

// TestBuildBlockStampsExecutionRoot is the builder-side always-active proof: the
// builder always stamps the xvm execution_root computed over the post-block
// state. The stamped root must be non-empty and must equal the canonical
// BlockExecutionRoot over the same inputs — there is no empty-root path.
func TestBuildBlockStampsExecutionRoot(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	parentRoot := ids.GenerateTestID()
	const parentHeight = uint64(41)

	built := buildOneTxBlock(t, ctrl, parentRoot, parentHeight)

	require.NotEqual(ids.Empty, built.MerkleRoot(), "builder must stamp a non-empty execution_root")

	// Recompute the expected root from the canonical projection: parent root +
	// the block's tx family at the built height (parentHeight + 1), over a
	// post-block state whose occupied UTXO set is empty (this unit's tx produces
	// none — same as the parent state the builder projected). The asset family is
	// always empty (xvm's UTXO-only executor state has no asset arena).
	emptyState := statemock.NewChain(ctrl)
	emptyState.EXPECT().UTXOs(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	want, err := blkexecutor.BlockExecutionRoot(parentRoot, built.Txs(), emptyState, parentHeight+1)
	require.NoError(err)
	require.Equal(want, built.MerkleRoot())
}

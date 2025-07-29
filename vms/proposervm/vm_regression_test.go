// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/engine/chain/block/blocktest"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/database/prefixdb"
)

func TestProposerVMInitializeShouldFailIfInnerVMCantVerifyItsHeightIndex(t *testing.T) {
	require := require.New(t)

	customError := errors.New("custom error")
	innerVM := &fullVM{
		VM: &blocktest.VM{
			VerifyHeightIndexF: func(_ context.Context) error {
				return customError
			},
		},
	}

	innerVM.InitializeF = func(context.Context, *consensus.Context, database.Database,
		[]byte, []byte, []byte,
		[]*core.Fx, core.AppSender,
	) error {
		return nil
	}

	proVM := New(
		innerVM,
		Config{
			ActivationTime:      time.Time{},
			DurangoTime:         time.Time{},
			MinimumPChainHeight: 0,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
		},
	)
	defer func() {
		// avoids leaking goroutines
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	ctx := consensustest.Context(t, consensustest.CChainID)
	initialState := []byte("genesis state")

	err := proVM.Initialize(
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()),
		initialState,
		nil,
		nil,
		nil,
		nil,
	)
	require.ErrorIs(customError, err)
}

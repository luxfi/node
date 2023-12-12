// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/database"
	"github.com/luxdefi/node/database/memdb"
	"github.com/luxdefi/node/database/prefixdb"
	"github.com/luxdefi/node/snow"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/snowman/block"
)

func TestProposerVMInitializeShouldFailIfInnerVMCantVerifyItsHeightIndex(t *testing.T) {
	require := require.New(t)

	innerVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
	}

	// let innerVM fail verifying its height index with
	// a non-special error (like block.ErrIndexIncomplete)
	customError := errors.New("custom error")
	innerVM.VerifyHeightIndexF = func(_ context.Context) error {
		return customError
	}

	innerVM.InitializeF = func(context.Context, *snow.Context, database.Database,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}

	proVM := New(
		innerVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		DefaultNumHistoricalBlocks,
		pTestSigner,
		pTestCert,
	)
	defer func() {
		// avoids leaking goroutines
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	ctx := snow.DefaultContextTest()
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
		nil,
	)
	require.ErrorIs(customError, err)
}

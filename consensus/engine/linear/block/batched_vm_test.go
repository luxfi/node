// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/linear/block/blocktest"
	"github.com/luxfi/node/consensus/linear"
	luxlog "github.com/luxfi/log"

	. "github.com/luxfi/node/consensus/engine/linear/block"
)

var errTest = errors.New("non-nil error")

func TestGetAncestorsDatabaseNotFound(t *testing.T) {
	require := require.New(t)

	vm := &blocktest.VM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (linear.Block, error) {
		require.Equal(someID, id)
		return nil, database.ErrNotFound
	}
	containers, err := GetAncestors(context.Background(), luxlog.NewNoOpLogger(){}, vm, someID, 10, 10, 1*time.Second)
	require.NoError(err)
	require.Empty(containers)
}

// TestGetAncestorsPropagatesErrors checks errors other than
// database.ErrNotFound propagate to caller.
func TestGetAncestorsPropagatesErrors(t *testing.T) {
	require := require.New(t)

	vm := &blocktest.VM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (linear.Block, error) {
		require.Equal(someID, id)
		return nil, errTest
	}
	containers, err := GetAncestors(context.Background(), luxlog.NewNoOpLogger(){}, vm, someID, 10, 10, 1*time.Second)
	require.Nil(containers)
	require.ErrorIs(err, errTest)
}

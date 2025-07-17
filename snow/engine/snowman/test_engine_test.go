// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common"
)

var (
	_ Engine = (*EngineTest)(nil)

	errGetBlock = errors.New("unexpectedly called GetBlock")
)

// EngineTest is a test engine
type EngineTest struct {
	*common.EngineTest

	CantGetBlock bool
	GetBlockF    func(context.Context, ids.ID) (snowman.Block, error)
}

func (e *EngineTest) Default(cant bool) {
	if e.EngineTest != nil {
		e.EngineTest.Default(cant)
	}
	e.CantGetBlock = false
}

func (e *EngineTest) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if e.GetBlockF != nil {
		return e.GetBlockF(ctx, blkID)
	}
	if e.CantGetBlock && e.EngineTest != nil && e.EngineTest.T != nil {
		require.FailNow(e.EngineTest.T, errGetBlock.Error())
	}
	return nil, errGetBlock
}
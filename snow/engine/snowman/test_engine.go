// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/snowman"
	"github.com/luxdefi/node/snow/engine/common"
)

var (
	_ Engine = (*EngineTest)(nil)

	errGetBlock = errors.New("unexpectedly called GetBlock")
)

// EngineTest is a test engine
type EngineTest struct {
	common.EngineTest

	CantGetBlock bool
	GetBlockF    func(context.Context, ids.ID) (snowman.Block, error)
}

func (e *EngineTest) Default(cant bool) {
	e.EngineTest.Default(cant)
	e.CantGetBlock = false
}

func (e *EngineTest) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if e.GetBlockF != nil {
		return e.GetBlockF(ctx, blkID)
	}
	if e.CantGetBlock && e.T != nil {
		require.FailNow(e.T, errGetBlock.Error())
	}
	return nil, errGetBlock
}

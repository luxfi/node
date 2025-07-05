// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/consensus/snowman"
)

var errGetBlock = errors.New("unexpectedly called GetBlock")

// EngineTest is a test engine
type EngineTest struct {
	*Engine

	CantGetBlock bool
	GetBlockF    func(context.Context, ids.ID) (snowman.Block, error)
}

func (e *EngineTest) Default(cant bool) {
	e.CantGetBlock = cant
}

func (e *EngineTest) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if e.GetBlockF != nil {
		return e.GetBlockF(ctx, blkID)
	}
	if e.CantGetBlock {
		return nil, errGetBlock
	}
	return nil, errGetBlock
}
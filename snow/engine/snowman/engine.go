// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common"
)

// Engine is an alias for Transitive for backward compatibility
type Engine = Transitive

// EngineInterface defines the snowman Engine interface that test mocks can implement
type EngineInterface interface {
	common.Engine
	GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error)
}
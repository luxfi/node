// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"reflect"

	"github.com/luxfi/consensus/snow"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/fxs"
)

type Backend struct {
	Ctx           context.Context
	SnowCtx       *snow.Context // Snow context for tests
	Config        *config.Config
	Fxs           []*fxs.ParsedFx
	TypeToFxIndex map[reflect.Type]int
	Codec         codec.Manager
	// Note: FeeAssetID may be different than ctx.LUXAssetID if this XVM is
	// running in a subnet.
	FeeAssetID   ids.ID
	Bootstrapped bool

	// Chain IDs for cross-chain operations
	XChainID ids.ID
	CChainID ids.ID

	// Logger for this backend
	Log log.Logger

	// SharedMemory provides cross-chain atomic operations
	SharedMemory SharedMemory
}

// SharedMemory interface for cross-chain operations
type SharedMemory interface {
	Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
	Apply(requests map[ids.ID]interface{}, batch ...interface{}) error
}

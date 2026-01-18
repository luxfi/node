// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"reflect"

	"github.com/luxfi/codec"
	"github.com/luxfi/runtime"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/exchangevm/config"
	"github.com/luxfi/node/vms/exchangevm/fxs"
)

type Backend struct {
	Ctx           context.Context
	Runtime       *runtime.Runtime
	Config        *config.Config
	Fxs           []*fxs.ParsedFx
	TypeToFxIndex map[reflect.Type]int
	Codec         codec.Manager
	// Note: FeeAssetID may be different than ctx.XAssetID if this XVM is
	// running in a chain.
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

// ToChainContext creates a verify.ChainContext from this backend
func (b *Backend) ToChainContext() *verify.ChainContext {
	var (
		chainID ids.ID
		netID   ids.ID
		vs      runtime.ValidatorState
	)

	if b.Runtime != nil {
		chainID = b.Runtime.ChainID
		if runtimeVS, ok := b.Runtime.ValidatorState.(runtime.ValidatorState); ok {
			vs = runtimeVS
			if resolvedNetID, err := runtimeVS.GetNetworkID(chainID); err == nil {
				netID = resolvedNetID
			}
		}
	}

	return &verify.ChainContext{
		ChainID:        chainID,
		NetID:          netID,
		ValidatorState: vs,
	}
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"sync"

	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/utxo"
)

type Backend struct {
	Config       *config.Config
	Ctx          context.Context
	LUXAssetID   ids.ID
	NodeID       ids.NodeID
	SharedMemory SharedMemory
	Lock         sync.Locker
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Calculator
	Rewards      reward.Calculator
	Bootstrapped *utils.Atomic[bool]
}

// SharedMemory provides cross-chain atomic operations
type SharedMemory interface {
	Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error)
	Apply(requests map[ids.ID]interface{}, batch ...interface{}) error
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/runtime"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/utxo"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utils"
	"github.com/luxfi/node/vms/platformvm/fx"
)

type Backend struct {
	Config       *config.Internal
	Runtime      *runtime.Runtime
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Calculator
	Rewards      reward.Calculator
	Bootstrapped *utils.Atomic[bool]
	Log          log.Logger
}

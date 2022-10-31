// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/snow/uptime"
	"github.com/luxdefi/luxd/utils"
	"github.com/luxdefi/luxd/utils/timer/mockable"
	"github.com/luxdefi/luxd/vms/platformvm/config"
	"github.com/luxdefi/luxd/vms/platformvm/fx"
	"github.com/luxdefi/luxd/vms/platformvm/reward"
	"github.com/luxdefi/luxd/vms/platformvm/utxo"
)

type Backend struct {
	Config       *config.Config
	Ctx          *snow.Context
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Manager
	Rewards      reward.Calculator
	Bootstrapped *utils.AtomicBool
}

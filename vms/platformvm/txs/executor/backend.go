// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxdefi/node/snow"
	"github.com/luxdefi/node/snow/uptime"
	"github.com/luxdefi/node/utils"
	"github.com/luxdefi/node/utils/timer/mockable"
	"github.com/luxdefi/node/vms/platformvm/config"
	"github.com/luxdefi/node/vms/platformvm/fx"
	"github.com/luxdefi/node/vms/platformvm/reward"
	"github.com/luxdefi/node/vms/platformvm/utxo"
)

type Backend struct {
	Config       *config.Config
	Ctx          *snow.Context
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Manager
	Rewards      reward.Calculator
	Bootstrapped *utils.Atomic[bool]
}

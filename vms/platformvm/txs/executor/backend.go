// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/uptime"
	"github.com/luxfi/node/v2/utils"
	"github.com/luxfi/node/v2/utils/timer/mockable"
	"github.com/luxfi/node/v2/vms/platformvm/config"
	"github.com/luxfi/node/v2/vms/platformvm/fx"
	"github.com/luxfi/node/v2/vms/platformvm/reward"
	"github.com/luxfi/node/v2/vms/platformvm/utxo"
)

type Backend struct {
	Config       *config.Internal
	Ctx          *quasar.Context
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Calculator
	Rewards      reward.Calculator
	Bootstrapped *utils.Atomic[bool]
}

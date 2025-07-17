// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statetest

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/validators"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
)

var DefaultNodeID = ids.GenerateTestNodeID()

type Config struct {
	DB              database.Database
	Genesis         []byte
	Registerer      prometheus.Registerer
	Validators      validators.Manager
	Upgrades        upgrade.Config
	ExecutionConfig config.ExecutionConfig
	Context         *snow.Context
	Metrics         metrics.Metrics
	Rewards         reward.Calculator
}

func New(t testing.TB, c Config) state.State {
	if c.DB == nil {
		c.DB = memdb.New()
	}
	if len(c.Genesis) == 0 {
		c.Genesis = genesistest.NewBytes(t, genesistest.Config{})
	}
	if c.Registerer == nil {
		c.Registerer = prometheus.NewRegistry()
	}
	if c.Validators == nil {
		c.Validators = validators.NewManager()
	}
	if c.Upgrades == (upgrade.Config{}) {
		c.Upgrades = upgradetest.GetConfig(upgradetest.Latest)
	}
	if c.ExecutionConfig == (config.ExecutionConfig{}) {
		c.ExecutionConfig = config.DefaultExecutionConfig
	}
	if c.Context == nil {
		c.Context = &snow.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    DefaultNodeID,
			Log:       logging.NoLog{},
		}
	}
	if c.Metrics == nil {
		c.Metrics = metrics.Noop
	}
	if c.Rewards == nil {
		c.Rewards = reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		})
	}

	s, err := state.New(
		c.DB,
		c.Genesis,
		c.Registerer,
		c.Validators,
		c.Upgrades,
		&c.ExecutionConfig,
		c.Context,
		c.Metrics,
		c.Rewards,
	)
	require.NoError(t, err)
	return s
}

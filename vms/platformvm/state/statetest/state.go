// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statetest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"

	// "github.com/luxfi/node/snow" // snow package removed
	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
)

var DefaultNodeID = ids.GenerateTestNodeID()

type Config struct {
	DB         database.Database
	Genesis    []byte
	Registerer prometheus.Registerer
	Validators validators.Manager
	Upgrades   upgrade.Config
	Config     config.Config
	Context    context.Context
	Metrics    metrics.Metrics
	Rewards    reward.Calculator
}

func New(t testing.TB, c Config) state.State {
	if c.DB == nil {
		c.DB = memdb.New()
	}
	if c.Context == nil {
		ctx := context.Background()
		ctx = consensus.WithNetworkID(ctx, constants.UnitTestID)
		ctx = consensus.WithNodeID(ctx, DefaultNodeID)
		ctx = consensus.WithLogger(ctx, consensus.NoOpLogger{})
		c.Context = ctx
	}
	if len(c.Genesis) == 0 {
		c.Genesis = genesistest.NewBytes(t, genesistest.Config{
			NetworkID: consensus.GetNetworkID(c.Context),
		})
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
	// Initialize fee configuration if not set
	if c.Config.StaticFeeConfig.CreateSubnetTxFee == 0 {
		c.Config.StaticFeeConfig.CreateSubnetTxFee = 1 * units.MilliLux
		c.Config.StaticFeeConfig.CreateBlockchainTxFee = 1 * units.MilliLux
	}
	if c.Metrics == nil {
		c.Metrics = metric.Noop
	}
	if c.Rewards == nil {
		c.Rewards = reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaLux,
		})
	}

	execCfg := &config.ExecutionConfig{
		BlockCacheSize:               64,
		TxCacheSize:                  128,
		TransformedSubnetTxCacheSize: 64,
		RewardUTXOsCacheSize:         2048,
		ChainCacheSize:               2048,
		ChainDBCacheSize:             2048,
		BlockIDCacheSize:             8192,
		FxOwnerCacheSize:             4 * 1024 * 1024,
	}

	s, err := state.New(
		c.DB,
		c.Genesis,
		c.Registerer,
		&c.Config,
		execCfg,
		c.Context,
		c.Metrics,
		c.Rewards,
	)
	require.NoError(t, err)
	return s
}

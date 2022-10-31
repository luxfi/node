// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxdefi/luxd/database/memdb"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowball"
	"github.com/luxdefi/luxd/snow/engine/lux/bootstrap"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/engine/common/queue"
)

func DefaultConfig() (common.Config, bootstrap.Config, Config) {
	vtxBlocked, _ := queue.NewWithMissing(memdb.New(), "", prometheus.NewRegistry())
	txBlocked, _ := queue.New(memdb.New(), "", prometheus.NewRegistry())

	commonCfg := common.DefaultConfigTest()

	bootstrapConfig := bootstrap.Config{
		Config:     commonCfg,
		VtxBlocked: vtxBlocked,
		TxBlocked:  txBlocked,
		Manager:    &vertex.TestManager{},
		VM:         &vertex.TestVM{},
	}

	engineConfig := Config{
		Ctx:        bootstrapConfig.Ctx,
		VM:         bootstrapConfig.VM,
		Manager:    bootstrapConfig.Manager,
		Sender:     bootstrapConfig.Sender,
		Validators: bootstrapConfig.Validators,
		Params: lux.Parameters{
			Parameters: snowball.Parameters{
				K:                       1,
				Alpha:                   1,
				BetaVirtuous:            1,
				BetaRogue:               2,
				ConcurrentRepolls:       1,
				OptimalProcessing:       100,
				MaxOutstandingItems:     1,
				MaxItemProcessingTime:   1,
				MixedQueryNumPushVdr:    1,
				MixedQueryNumPushNonVdr: 1,
			},
			Parents:   2,
			BatchSize: 1,
		},
		Consensus: &lux.Topological{},
	}

	return commonCfg, bootstrapConfig, engineConfig
}

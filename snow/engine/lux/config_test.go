<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package lux

import (
	"github.com/prometheus/client_golang/prometheus"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/config_test.go
	"github.com/luxdefi/node/database/memdb"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/consensus/snowball"
	"github.com/luxdefi/node/snow/engine/avalanche/bootstrap"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/queue"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/node/database/memdb"
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/snow/consensus/snowball"
	"github.com/luxdefi/node/snow/engine/lux/bootstrap"
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/queue"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/config_test.go
>>>>>>> 53a8245a8 (Update consensus)
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

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	"testing"

	"github.com/luxfi/node/consensus/factories"
	"github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/engine/core/tracker"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/consensus/engine/linear/block/blocktest"
	snowtest "github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/validators"
)

func DefaultConfig(t testing.TB) Config {
	ctx := snowtest.Context(t, snowtest.PChainID)

	return Config{
		Ctx:                 snowtest.ConsensusContext(ctx),
		VM:                  &blocktest.VM{},
		Sender:              &enginetest.Sender{},
		Validators:          validators.NewManager(),
		ConnectedValidators: tracker.NewPeers(),
		Params: sampling.Parameters{
			K:                     1,
			AlphaPreference:       1,
			AlphaConfidence:       1,
			Beta:                  1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     100,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: &linear.Topological{Factory: factories.SnowflakeFactory},
	}
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	"github.com/luxfi/node/consensus"
	sampling "github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/engine/core/tracker"
	"github.com/luxfi/node/consensus/engine/linear/block"
	"github.com/luxfi/node/consensus/validators"
)

// Config wraps all the parameters needed for a linear engine
type Config struct {
	core.AllGetsServer

	Ctx                 *consensus.Context
	VM                  block.ChainVM
	Sender              core.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              sampling.Parameters
	Consensus           linear.Consensus
	PartialSync         bool
}

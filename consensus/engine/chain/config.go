// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/node/consensus"
	sampling "github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/engine"
	"github.com/luxfi/node/consensus/engine/tracker"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	engine.AllGetsServer

	Ctx                 *consensus.Context
	VM                  block.ChainVM
	Sender              engine.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              sampling.Parameters
	Consensus           chain.Consensus
	PartialSync         bool
}

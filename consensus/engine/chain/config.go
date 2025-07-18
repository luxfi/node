// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/binaryvote"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/engine/common"
	"github.com/luxfi/node/consensus/engine/common/tracker"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	common.AllGetsServer

	Ctx                 *snow.ConsensusContext
	VM                  block.ChainVM
	Sender              common.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              binaryvote.Parameters
	Consensus           chain.Consensus
	PartialSync         bool
}

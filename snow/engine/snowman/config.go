// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/consensus/snowball"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/snow/engine/common/tracker"
	"github.com/luxfi/node/snow/engine/snowman/block"
	"github.com/luxfi/node/snow/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	common.AllGetsServer

	Ctx                 *snow.ConsensusContext
	VM                  block.ChainVM
	Sender              common.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              snowball.Parameters
	Consensus           snowman.Consensus
	PartialSync         bool
}

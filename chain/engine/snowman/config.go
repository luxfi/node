// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/chain/consensus/snowball"
	"github.com/luxfi/node/chain/consensus/snowman"
	"github.com/luxfi/node/chain/engine/common"
	"github.com/luxfi/node/chain/engine/common/tracker"
	"github.com/luxfi/node/chain/engine/snowman/block"
	"github.com/luxfi/node/chain/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	core.AllGetsServer

	Ctx                 *snow.ConsensusContext
	VM                  block.ChainVM
	Sender              core.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              snowball.Parameters
	Consensus           chain.Consensus
	PartialSync         bool
}

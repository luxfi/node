// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/snow/consensus/snowball"
	"github.com/luxdefi/luxd/snow/consensus/snowman"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/engine/snowman/block"
	"github.com/luxdefi/luxd/snow/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	common.AllGetsServer

	Ctx        *snow.ConsensusContext
	VM         block.ChainVM
	Sender     common.Sender
	Validators validators.Set
	Params     snowball.Parameters
	Consensus  snowman.Consensus
}

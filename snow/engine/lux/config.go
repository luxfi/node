// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"github.com/luxdefi/node/snow"
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/validators"
)

// Config wraps all the parameters needed for an lux engine
type Config struct {
	Ctx *snow.ConsensusContext
	common.AllGetsServer
	VM         vertex.DAGVM
	Manager    vertex.Manager
	Sender     common.Sender
	Validators validators.Set

	Params    lux.Parameters
	Consensus lux.Consensus
}

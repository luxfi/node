<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/config.go
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/validators"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/config.go
>>>>>>> 53a8245a8 (Update consensus)
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

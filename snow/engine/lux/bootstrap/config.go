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

package bootstrap

import (
<<<<<<< HEAD
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/queue"
=======
<<<<<<< HEAD:snow/engine/avalanche/bootstrap/config.go
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/queue"
=======
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/queue"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/bootstrap/config.go
>>>>>>> 53a8245a8 (Update consensus)
)

type Config struct {
	common.Config
	common.AllGetsServer

	// VtxBlocked tracks operations that are blocked on vertices
	VtxBlocked *queue.JobsWithMissing
	// TxBlocked tracks operations that are blocked on transactions
	TxBlocked *queue.Jobs

	Manager vertex.Manager
	VM      vertex.DAGVM
}

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

package lux

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
=======
<<<<<<< HEAD:snow/engine/avalanche/engine.go
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/engine.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Engine describes the events that can occur on a consensus instance
type Engine interface {
	common.Engine

	// GetVtx returns a vertex by its ID.
	// Returns an error if unknown.
<<<<<<< HEAD
	GetVtx(vtxID ids.ID) (lux.Vertex, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/engine.go
	GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error)
=======
	GetVtx(vtxID ids.ID) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/engine.go
>>>>>>> 53a8245a8 (Update consensus)
}

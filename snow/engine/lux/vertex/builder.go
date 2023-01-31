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

package vertex

import (
<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/builder.go
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/utils/hashing"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/builder.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Builder builds a vertex given a set of parentIDs and transactions.
type Builder interface {
	// Build a new vertex from the contents of a vertex
<<<<<<< HEAD
	BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error)
	// Build a new stop vertex from the parents
	BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/builder.go
	BuildVtx(ctx context.Context, parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error)
	// Build a new stop vertex from the parents
	BuildStopVtx(ctx context.Context, parentIDs []ids.ID) (avalanche.Vertex, error)
=======
	BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error)
	// Build a new stop vertex from the parents
	BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/builder.go
>>>>>>> 53a8245a8 (Update consensus)
}

// Build a new stateless vertex from the contents of a vertex
func Build(
	chainID ids.ID,
	height uint64,
	parentIDs []ids.ID,
	txs [][]byte,
) (StatelessVertex, error) {
	return buildVtx(
		chainID,
		height,
		parentIDs,
		txs,
		func(vtx innerStatelessVertex) error {
			return vtx.verify()
		},
		false,
	)
}

// Build a new stateless vertex from the contents of a vertex
func BuildStopVertex(chainID ids.ID, height uint64, parentIDs []ids.ID) (StatelessVertex, error) {
	return buildVtx(
		chainID,
		height,
		parentIDs,
		nil,
		func(vtx innerStatelessVertex) error {
			return vtx.verifyStopVertex()
		},
		true,
	)
}

func buildVtx(
	chainID ids.ID,
	height uint64,
	parentIDs []ids.ID,
	txs [][]byte,
	verifyFunc func(innerStatelessVertex) error,
	stopVertex bool,
) (StatelessVertex, error) {
<<<<<<< HEAD
	ids.SortIDs(parentIDs)
	SortHashOf(txs)
=======
	utils.Sort(parentIDs)
	utils.SortByHash(txs)
>>>>>>> 53a8245a8 (Update consensus)

	codecVer := codecVersion
	if stopVertex {
		// use new codec version for the "StopVertex"
		codecVer = codecVersionWithStopVtx
	}

	innerVtx := innerStatelessVertex{
		Version:   codecVer,
		ChainID:   chainID,
		Height:    height,
		Epoch:     0,
		ParentIDs: parentIDs,
		Txs:       txs,
	}
	if err := verifyFunc(innerVtx); err != nil {
		return nil, err
	}

	vtxBytes, err := c.Marshal(innerVtx.Version, innerVtx)
	vtx := statelessVertex{
		innerStatelessVertex: innerVtx,
		id:                   hashing.ComputeHash256Array(vtxBytes),
		bytes:                vtxBytes,
	}
	return vtx, err
}

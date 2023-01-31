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

// Package state manages the meta-data required by consensus for an lux
// dag.
package state

import (
<<<<<<< HEAD
	"errors"
	"time"

=======
	"context"
	"errors"
	"time"

<<<<<<< HEAD:snow/engine/avalanche/state/serializer.go
	"github.com/luxdefi/node/cache"
	"github.com/luxdefi/node/database"
	"github.com/luxdefi/node/database/versiondb"
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/utils/logging"
	"github.com/luxdefi/node/utils/math"
	"github.com/luxdefi/node/utils/set"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/node/cache"
	"github.com/luxdefi/node/database"
	"github.com/luxdefi/node/database/versiondb"
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/utils/logging"
	"github.com/luxdefi/node/utils/math"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/state/serializer.go
>>>>>>> 53a8245a8 (Update consensus)
)

const (
	dbCacheSize = 10000
	idCacheSize = 1000
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errWrongChainID  = errors.New("wrong ChainID in vertex")
)

var _ vertex.Manager = (*Serializer)(nil)

// Serializer manages the state of multiple vertices
type Serializer struct {
	SerializerConfig
	versionDB *versiondb.Database
	state     *prefixedState
<<<<<<< HEAD
	edge      ids.Set
=======
	edge      set.Set[ids.ID]
>>>>>>> 53a8245a8 (Update consensus)
}

type SerializerConfig struct {
	ChainID             ids.ID
	VM                  vertex.DAGVM
	DB                  database.Database
	Log                 logging.Logger
	XChainMigrationTime time.Time
}

func NewSerializer(config SerializerConfig) vertex.Manager {
	versionDB := versiondb.New(config.DB)
	dbCache := &cache.LRU{Size: dbCacheSize}
	s := Serializer{
		SerializerConfig: config,
		versionDB:        versionDB,
	}

	rawState := &state{
		serializer: &s,
		log:        config.Log,
		dbCache:    dbCache,
		db:         versionDB,
	}

	s.state = newPrefixedState(rawState, idCacheSize)
	s.edge.Add(s.state.Edge()...)

	return &s
}

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/state/serializer.go
func (s *Serializer) ParseVtx(ctx context.Context, b []byte) (avalanche.Vertex, error) {
	return newUniqueVertex(ctx, s, b)
}

func (s *Serializer) BuildVtx(
	ctx context.Context,
	parentIDs []ids.ID,
	txs []snowstorm.Tx,
) (avalanche.Vertex, error) {
	return s.buildVtx(ctx, parentIDs, txs, false)
}

func (s *Serializer) BuildStopVtx(
	ctx context.Context,
	parentIDs []ids.ID,
) (avalanche.Vertex, error) {
	return s.buildVtx(ctx, parentIDs, nil, true)
=======
>>>>>>> 53a8245a8 (Update consensus)
func (s *Serializer) ParseVtx(b []byte) (lux.Vertex, error) {
	return newUniqueVertex(s, b)
}

func (s *Serializer) BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
	return s.buildVtx(parentIDs, txs, false)
}

func (s *Serializer) BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error) {
	return s.buildVtx(parentIDs, nil, true)
<<<<<<< HEAD
}

func (s *Serializer) buildVtx(
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/state/serializer.go
}

func (s *Serializer) buildVtx(
	ctx context.Context,
>>>>>>> 53a8245a8 (Update consensus)
	parentIDs []ids.ID,
	txs []snowstorm.Tx,
	stopVtx bool,
) (lux.Vertex, error) {
	height := uint64(0)
	for _, parentID := range parentIDs {
		parent, err := s.getUniqueVertex(parentID)
		if err != nil {
			return nil, err
		}
		parentHeight := parent.v.vtx.Height()
		childHeight, err := math.Add64(parentHeight, 1)
		if err != nil {
			return nil, err
		}
		height = math.Max(height, childHeight)
	}

	var (
		vtx vertex.StatelessVertex
		err error
	)
	if !stopVtx {
		txBytes := make([][]byte, len(txs))
		for i, tx := range txs {
			txBytes[i] = tx.Bytes()
		}
		vtx, err = vertex.Build(
			s.ChainID,
			height,
			parentIDs,
			txBytes,
		)
	} else {
		vtx, err = vertex.BuildStopVertex(
			s.ChainID,
			height,
			parentIDs,
		)
	}
	if err != nil {
		return nil, err
	}

	uVtx := &uniqueVertex{
		serializer: s,
		id:         vtx.ID(),
	}
	// setVertex handles the case where this vertex already exists even
	// though we just made it
<<<<<<< HEAD
	return uVtx, uVtx.setVertex(vtx)
}

func (s *Serializer) GetVtx(vtxID ids.ID) (lux.Vertex, error) {
	return s.getUniqueVertex(vtxID)
}

func (s *Serializer) Edge() []ids.ID { return s.edge.List() }
=======
	return uVtx, uVtx.setVertex(ctx, vtx)
}

<<<<<<< HEAD:snow/engine/avalanche/state/serializer.go
func (s *Serializer) GetVtx(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
func (s *Serializer) GetVtx(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/state/serializer.go
	return s.getUniqueVertex(vtxID)
}

<<<<<<< HEAD
<<<<<<< HEAD
func (s *Serializer) Edge(context.Context) []ids.ID {
=======
func (s *Serializer) Edge() []ids.ID {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (s *Serializer) Edge(context.Context) []ids.ID {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
	return s.edge.List()
}
>>>>>>> 53a8245a8 (Update consensus)

func (s *Serializer) parseVertex(b []byte) (vertex.StatelessVertex, error) {
	vtx, err := vertex.Parse(b)
	if err != nil {
		return nil, err
	}
	if vtx.ChainID() != s.ChainID {
		return nil, errWrongChainID
	}
	return vtx, nil
}

func (s *Serializer) getUniqueVertex(vtxID ids.ID) (*uniqueVertex, error) {
	vtx := &uniqueVertex{
		serializer: s,
		id:         vtxID,
	}
	if vtx.Status() == choices.Unknown {
		return nil, errUnknownVertex
	}
	return vtx, nil
}

<<<<<<< HEAD
func (s *Serializer) StopVertexAccepted() (bool, error) {
	edge := s.Edge()
=======
func (s *Serializer) StopVertexAccepted(ctx context.Context) (bool, error) {
	edge := s.Edge(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	if len(edge) != 1 {
		return false, nil
	}

	vtx, err := s.getUniqueVertex(edge[0])
	if err != nil {
		return false, err
	}

	return vtx.v.vtx.StopVertex(), nil
}

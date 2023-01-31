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
	"errors"
	"testing"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/lux"
=======
	"context"
	"errors"
	"testing"

<<<<<<< HEAD:snow/engine/avalanche/vertex/test_storage.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/avalanche"
=======
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/lux"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_storage.go
>>>>>>> 53a8245a8 (Update consensus)
)

var (
	errGet                = errors.New("unexpectedly called Get")
	errEdge               = errors.New("unexpectedly called Edge")
	errStopVertexAccepted = errors.New("unexpectedly called StopVertexAccepted")

	_ Storage = (*TestStorage)(nil)
)

type TestStorage struct {
	T                                            *testing.T
	CantGetVtx, CantEdge, CantStopVertexAccepted bool
<<<<<<< HEAD
	GetVtxF                                      func(ids.ID) (lux.Vertex, error)
	EdgeF                                        func() []ids.ID
	StopVertexAcceptedF                          func() (bool, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/test_storage.go
	GetVtxF                                      func(context.Context, ids.ID) (avalanche.Vertex, error)
	EdgeF                                        func(context.Context) []ids.ID
	StopVertexAcceptedF                          func(context.Context) (bool, error)
=======
	GetVtxF                                      func(ids.ID) (lux.Vertex, error)
	EdgeF                                        func() []ids.ID
	StopVertexAcceptedF                          func() (bool, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_storage.go
>>>>>>> 53a8245a8 (Update consensus)
}

func (s *TestStorage) Default(cant bool) {
	s.CantGetVtx = cant
	s.CantEdge = cant
}

<<<<<<< HEAD
func (s *TestStorage) GetVtx(id ids.ID) (lux.Vertex, error) {
	if s.GetVtxF != nil {
		return s.GetVtxF(id)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/test_storage.go
func (s *TestStorage) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
func (s *TestStorage) GetVtx(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_storage.go
	if s.GetVtxF != nil {
		return s.GetVtxF(ctx, vtxID)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if s.CantGetVtx && s.T != nil {
		s.T.Fatal(errGet)
	}
	return nil, errGet
}

<<<<<<< HEAD
func (s *TestStorage) Edge() []ids.ID {
	if s.EdgeF != nil {
		return s.EdgeF()
=======
func (s *TestStorage) Edge(ctx context.Context) []ids.ID {
	if s.EdgeF != nil {
		return s.EdgeF(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if s.CantEdge && s.T != nil {
		s.T.Fatal(errEdge)
	}
	return nil
}

<<<<<<< HEAD
func (s *TestStorage) StopVertexAccepted() (bool, error) {
	if s.StopVertexAcceptedF != nil {
		return s.StopVertexAcceptedF()
=======
func (s *TestStorage) StopVertexAccepted(ctx context.Context) (bool, error) {
	if s.StopVertexAcceptedF != nil {
		return s.StopVertexAcceptedF(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if s.CantStopVertexAccepted && s.T != nil {
		s.T.Fatal(errStopVertexAccepted)
	}
	return false, nil
}

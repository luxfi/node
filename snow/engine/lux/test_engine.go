<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
	"errors"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
=======
	"context"
	"errors"

<<<<<<< HEAD:snow/engine/avalanche/test_engine.go
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/test_engine.go
>>>>>>> 53a8245a8 (Update consensus)
)

var (
	_ Engine = (*EngineTest)(nil)

	errGetVtx = errors.New("unexpectedly called GetVtx")
)

// EngineTest is a test engine
type EngineTest struct {
	common.EngineTest

	CantGetVtx bool
<<<<<<< HEAD
	GetVtxF    func(vtxID ids.ID) (lux.Vertex, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/test_engine.go
	GetVtxF    func(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error)
=======
	GetVtxF    func(vtxID ids.ID) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/test_engine.go
>>>>>>> 53a8245a8 (Update consensus)
}

func (e *EngineTest) Default(cant bool) {
	e.EngineTest.Default(cant)
	e.CantGetVtx = false
}

<<<<<<< HEAD
func (e *EngineTest) GetVtx(vtxID ids.ID) (lux.Vertex, error) {
	if e.GetVtxF != nil {
		return e.GetVtxF(vtxID)
=======
<<<<<<< HEAD:snow/engine/avalanche/test_engine.go
func (e *EngineTest) GetVtx(ctx context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
func (e *EngineTest) GetVtx(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/test_engine.go
	if e.GetVtxF != nil {
		return e.GetVtxF(ctx, vtxID)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if e.CantGetVtx && e.T != nil {
		e.T.Fatalf("Unexpectedly called GetVtx")
	}
	return nil, errGetVtx
}

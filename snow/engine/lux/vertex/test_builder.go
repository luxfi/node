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

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
=======
	"context"
	"errors"
	"testing"

<<<<<<< HEAD:snow/engine/avalanche/vertex/test_builder.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_builder.go
>>>>>>> 53a8245a8 (Update consensus)
)

var (
	errBuild = errors.New("unexpectedly called Build")

	_ Builder = (*TestBuilder)(nil)
)

type TestBuilder struct {
	T             *testing.T
	CantBuildVtx  bool
<<<<<<< HEAD
	BuildVtxF     func(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error)
	BuildStopVtxF func(parentIDs []ids.ID) (lux.Vertex, error)
}

func (b *TestBuilder) Default(cant bool) { b.CantBuildVtx = cant }

func (b *TestBuilder) BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
	if b.BuildVtxF != nil {
		return b.BuildVtxF(parentIDs, txs)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/test_builder.go
	BuildVtxF     func(ctx context.Context, parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error)
	BuildStopVtxF func(ctx context.Context, parentIDs []ids.ID) (avalanche.Vertex, error)
=======
	BuildVtxF     func(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error)
	BuildStopVtxF func(parentIDs []ids.ID) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_builder.go
}

func (b *TestBuilder) Default(cant bool) {
	b.CantBuildVtx = cant
}

<<<<<<< HEAD:snow/engine/avalanche/vertex/test_builder.go
func (b *TestBuilder) BuildVtx(ctx context.Context, parentIDs []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
=======
func (b *TestBuilder) BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_builder.go
	if b.BuildVtxF != nil {
		return b.BuildVtxF(ctx, parentIDs, txs)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}

<<<<<<< HEAD
func (b *TestBuilder) BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error) {
	if b.BuildStopVtxF != nil {
		return b.BuildStopVtxF(parentIDs)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/test_builder.go
func (b *TestBuilder) BuildStopVtx(ctx context.Context, parentIDs []ids.ID) (avalanche.Vertex, error) {
=======
func (b *TestBuilder) BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_builder.go
	if b.BuildStopVtxF != nil {
		return b.BuildStopVtxF(ctx, parentIDs)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}

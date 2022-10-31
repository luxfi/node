// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"testing"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
)

var (
	errBuild = errors.New("unexpectedly called Build")

	_ Builder = (*TestBuilder)(nil)
)

type TestBuilder struct {
	T             *testing.T
	CantBuildVtx  bool
	BuildVtxF     func(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error)
	BuildStopVtxF func(parentIDs []ids.ID) (lux.Vertex, error)
}

func (b *TestBuilder) Default(cant bool) { b.CantBuildVtx = cant }

func (b *TestBuilder) BuildVtx(parentIDs []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
	if b.BuildVtxF != nil {
		return b.BuildVtxF(parentIDs, txs)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}

func (b *TestBuilder) BuildStopVtx(parentIDs []ids.ID) (lux.Vertex, error) {
	if b.BuildStopVtxF != nil {
		return b.BuildStopVtxF(parentIDs)
	}
	if b.CantBuildVtx && b.T != nil {
		b.T.Fatal(errBuild)
	}
	return nil, errBuild
}

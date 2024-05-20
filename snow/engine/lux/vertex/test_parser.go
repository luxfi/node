// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/snow/consensus/lux"
)

var (
	errParse = errors.New("unexpectedly called Parse")

	_ Parser = (*TestParser)(nil)
)

type TestParser struct {
	T            *testing.T
	CantParseVtx bool
	ParseVtxF    func(context.Context, []byte) (lux.Vertex, error)
}

func (p *TestParser) Default(cant bool) {
	p.CantParseVtx = cant
}

func (p *TestParser) ParseVtx(ctx context.Context, b []byte) (lux.Vertex, error) {
	if p.ParseVtxF != nil {
		return p.ParseVtxF(ctx, b)
	}
	if p.CantParseVtx && p.T != nil {
		require.FailNow(p.T, errParse.Error())
	}
	return nil, errParse
}

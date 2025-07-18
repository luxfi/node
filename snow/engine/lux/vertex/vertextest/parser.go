// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/snow/consensus/lux"
	"github.com/luxfi/node/snow/engine/lux/vertex"
)

var (
	errParse = errors.New("unexpectedly called Parse")

	_ vertex.Parser = (*Parser)(nil)
)

type Parser struct {
	T            *testing.T
	CantParseVtx bool
	ParseVtxF    func(context.Context, []byte) (lux.Vertex, error)
}

func (p *Parser) Default(cant bool) {
	p.CantParseVtx = cant
}

func (p *Parser) ParseVtx(ctx context.Context, b []byte) (lux.Vertex, error) {
	if p.ParseVtxF != nil {
		return p.ParseVtxF(ctx, b)
	}
	if p.CantParseVtx && p.T != nil {
		require.FailNow(p.T, errParse.Error())
	}
	return nil, errParse
}

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

	"github.com/luxdefi/node/snow/consensus/lux"
=======
	"context"
	"errors"
	"testing"

<<<<<<< HEAD:snow/engine/avalanche/vertex/test_parser.go
	"github.com/luxdefi/node/snow/consensus/avalanche"
=======
	"github.com/luxdefi/node/snow/consensus/lux"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_parser.go
>>>>>>> 53a8245a8 (Update consensus)
)

var (
	errParse = errors.New("unexpectedly called Parse")

	_ Parser = (*TestParser)(nil)
)

type TestParser struct {
	T            *testing.T
	CantParseVtx bool
<<<<<<< HEAD
	ParseVtxF    func([]byte) (lux.Vertex, error)
}

func (p *TestParser) Default(cant bool) { p.CantParseVtx = cant }

func (p *TestParser) ParseVtx(b []byte) (lux.Vertex, error) {
	if p.ParseVtxF != nil {
		return p.ParseVtxF(b)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/test_parser.go
	ParseVtxF    func(context.Context, []byte) (avalanche.Vertex, error)
=======
	ParseVtxF    func([]byte) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_parser.go
}

func (p *TestParser) Default(cant bool) {
	p.CantParseVtx = cant
}

<<<<<<< HEAD:snow/engine/avalanche/vertex/test_parser.go
func (p *TestParser) ParseVtx(ctx context.Context, b []byte) (avalanche.Vertex, error) {
=======
func (p *TestParser) ParseVtx(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/test_parser.go
	if p.ParseVtxF != nil {
		return p.ParseVtxF(ctx, b)
>>>>>>> 53a8245a8 (Update consensus)
	}
	if p.CantParseVtx && p.T != nil {
		p.T.Fatal(errParse)
	}
	return nil, errParse
}

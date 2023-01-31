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
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/utils/hashing"
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/parser.go
	"context"

	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/utils/hashing"
=======
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/utils/hashing"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/parser.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Parser parses bytes into a vertex.
type Parser interface {
	// Parse a vertex from a slice of bytes
<<<<<<< HEAD
	ParseVtx(vertex []byte) (lux.Vertex, error)
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/parser.go
	ParseVtx(ctx context.Context, vertex []byte) (avalanche.Vertex, error)
=======
	ParseVtx(vertex []byte) (lux.Vertex, error)
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/parser.go
>>>>>>> 53a8245a8 (Update consensus)
}

// Parse parses the provided vertex bytes into a stateless vertex
func Parse(bytes []byte) (StatelessVertex, error) {
	vtx := innerStatelessVertex{}
	version, err := c.Unmarshal(bytes, &vtx)
	if err != nil {
		return nil, err
	}
	vtx.Version = version

	return statelessVertex{
		innerStatelessVertex: vtx,
		id:                   hashing.ComputeHash256Array(bytes),
		bytes:                bytes,
	}, nil
}

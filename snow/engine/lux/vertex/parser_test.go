<<<<<<< HEAD
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
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 8fb2bec88 (Must keep bloodline pure)
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
=======
	"github.com/luxdefi/node/ids"
>>>>>>> 53a8245a8 (Update consensus)
)

func TestParseInvalid(t *testing.T) {
	vtxBytes := []byte{}
	_, err := Parse(vtxBytes)
	require.Error(t, err, "parse on an invalid vertex should have errored")
}

func TestParseValid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{6}, {7}}
	vtx, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	require.NoError(t, err)

	vtxBytes := vtx.Bytes()
	parsedVtx, err := Parse(vtxBytes)
	require.NoError(t, err)
	require.Equal(t, vtx, parsedVtx)
}

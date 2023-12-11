// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/codec"
	"github.com/luxdefi/node/ids"
)

func TestParseInvalid(t *testing.T) {
	vtxBytes := []byte{1, 2, 3, 4, 5}
	_, err := Parse(vtxBytes)
	require.ErrorIs(t, err, codec.ErrUnknownVersion)
}

func TestParseValid(t *testing.T) {
	require := require.New(t)

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
	require.NoError(err)

	vtxBytes := vtx.Bytes()
	parsedVtx, err := Parse(vtxBytes)
	require.NoError(err)
	require.Equal(vtx, parsedVtx)
}

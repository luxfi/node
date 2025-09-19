// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
)

func FuzzMarshalDiffKey(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var (
			netID  ids.ID
			height uint64
			nodeID ids.NodeID
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&netID, &height, &nodeID)

		key := marshalDiffKey(netID, height, nodeID)
		parsedNetID, parsedHeight, parsedNodeID, err := unmarshalDiffKey(key)
		require.NoError(err)
		require.Equal(netID, parsedNetID)
		require.Equal(height, parsedHeight)
		require.Equal(nodeID, parsedNodeID)
	})
}

func FuzzUnmarshalDiffKey(f *testing.F) {
	f.Fuzz(func(t *testing.T, key []byte) {
		require := require.New(t)

		netID, height, nodeID, err := unmarshalDiffKey(key)
		if err != nil {
			require.ErrorIs(err, errUnexpectedDiffKeyLength)
			return
		}

		formattedKey := marshalDiffKey(netID, height, nodeID)
		require.Equal(key, formattedKey)
	})
}

func TestDiffIteration(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	netID0 := ids.GenerateTestID()
	netID1 := ids.GenerateTestID()

	nodeID0 := ids.BuildTestNodeID([]byte{0x00})
	nodeID1 := ids.BuildTestNodeID([]byte{0x01})

	netID0Height0NodeID0 := marshalDiffKey(netID0, 0, nodeID0)
	netID0Height1NodeID0 := marshalDiffKey(netID0, 1, nodeID0)
	netID0Height1NodeID1 := marshalDiffKey(netID0, 1, nodeID1)

	netID1Height0NodeID0 := marshalDiffKey(netID1, 0, nodeID0)
	netID1Height1NodeID0 := marshalDiffKey(netID1, 1, nodeID0)
	netID1Height1NodeID1 := marshalDiffKey(netID1, 1, nodeID1)

	require.NoError(db.Put(netID0Height0NodeID0, nil))
	require.NoError(db.Put(netID0Height1NodeID0, nil))
	require.NoError(db.Put(netID0Height1NodeID1, nil))
	require.NoError(db.Put(netID1Height0NodeID0, nil))
	require.NoError(db.Put(netID1Height1NodeID0, nil))
	require.NoError(db.Put(netID1Height1NodeID1, nil))

	{
		it := db.NewIteratorWithStartAndPrefix(marshalStartDiffKey(netID0, 0), netID0[:])
		defer it.Release()

		expectedKeys := [][]byte{
			netID0Height0NodeID0,
		}
		for _, expectedKey := range expectedKeys {
			require.True(it.Next())
			require.Equal(expectedKey, it.Key())
		}
		require.False(it.Next())
		require.NoError(it.Error())
	}

	{
		it := db.NewIteratorWithStartAndPrefix(marshalStartDiffKey(netID0, 1), netID0[:])
		defer it.Release()

		expectedKeys := [][]byte{
			netID0Height1NodeID0,
			netID0Height1NodeID1,
			netID0Height0NodeID0,
		}
		for _, expectedKey := range expectedKeys {
			require.True(it.Next())
			require.Equal(expectedKey, it.Key())
		}
		require.False(it.Next())
		require.NoError(it.Error())
	}
}

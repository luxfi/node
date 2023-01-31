// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
func TestAdd(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

<<<<<<< HEAD
	err := Add(m, subnetID, nodeID, nil, ids.Empty, 1)
=======
	err := Add(m, subnetID, nodeID, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

<<<<<<< HEAD
	err = Add(m, subnetID, nodeID, nil, ids.Empty, 1)
=======
	err = Add(m, subnetID, nodeID, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(1, weight)
}

<<<<<<< HEAD
=======
>>>>>>> f171d317d (Remove unnecessary functions from validators.Manager interface (#2277))
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
func TestAddWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	err := AddWeight(m, subnetID, nodeID, 1)
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

	err = AddWeight(m, subnetID, nodeID, 1)
<<<<<<< HEAD
<<<<<<< HEAD
	require.ErrorIs(err, errMissingValidator)

	err = Add(m, subnetID, nodeID, nil, ids.Empty, 1)
	require.NoError(err)

	err = AddWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(2, weight)
=======
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(1, weight)
>>>>>>> f171d317d (Remove unnecessary functions from validators.Manager interface (#2277))
=======
	require.ErrorIs(err, errMissingValidator)

	err = Add(m, subnetID, nodeID, 1)
	require.NoError(err)

	err = AddWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(2, weight)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
}

func TestRemoveWeight(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	err := RemoveWeight(m, subnetID, nodeID, 1)
	require.ErrorIs(err, errMissingValidators)

	s := NewSet()
	m.Add(subnetID, s)

<<<<<<< HEAD
<<<<<<< HEAD
	err = Add(m, subnetID, nodeID, nil, ids.Empty, 2)
=======
	err = AddWeight(m, subnetID, nodeID, 2)
>>>>>>> f171d317d (Remove unnecessary functions from validators.Manager interface (#2277))
=======
	err = Add(m, subnetID, nodeID, 2)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	require.NoError(err)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight := s.Weight()
	require.EqualValues(1, weight)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	weight = s.Weight()
	require.Zero(weight)
}

func TestContains(t *testing.T) {
	require := require.New(t)

	m := NewManager()

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()

	contains := Contains(m, subnetID, nodeID)
	require.False(contains)

	s := NewSet()
	m.Add(subnetID, s)

	contains = Contains(m, subnetID, nodeID)
	require.False(contains)

<<<<<<< HEAD
<<<<<<< HEAD
	err := Add(m, subnetID, nodeID, nil, ids.Empty, 1)
=======
	err := AddWeight(m, subnetID, nodeID, 1)
>>>>>>> f171d317d (Remove unnecessary functions from validators.Manager interface (#2277))
=======
	err := Add(m, subnetID, nodeID, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	require.NoError(err)

	contains = Contains(m, subnetID, nodeID)
	require.True(contains)

	err = RemoveWeight(m, subnetID, nodeID, 1)
	require.NoError(err)

	contains = Contains(m, subnetID, nodeID)
	require.False(contains)
}

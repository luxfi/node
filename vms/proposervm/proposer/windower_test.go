// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestWindowerNoValidators(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	vdrState := &validators.TestState{
		T: t,
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
=======
		GetValidatorSetF: func(uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.Validator, error) {
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
			return nil, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	delay, err := w.Delay(context.Background(), 1, 0, nodeID)
	require.NoError(err)
	require.EqualValues(0, delay)
}

func TestWindowerRepeatedValidator(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	validatorID := ids.GenerateTestNodeID()
	nonValidatorID := ids.GenerateTestNodeID()
	vdrState := &validators.TestState{
		T: t,
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.Validator, error) {
			return map[ids.NodeID]*validators.Validator{
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
				validatorID: {
					NodeID: validatorID,
					Weight: 10,
				},
<<<<<<< HEAD
=======
		GetValidatorSetF: func(uint64, ids.ID) (map[ids.NodeID]uint64, error) {
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
			return map[ids.NodeID]uint64{
				validatorID: 10,
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
			}, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	validatorDelay, err := w.Delay(context.Background(), 1, 0, validatorID)
	require.NoError(err)
	require.EqualValues(0, validatorDelay)

	nonValidatorDelay, err := w.Delay(context.Background(), 1, 0, nonValidatorID)
	require.NoError(err)
	require.EqualValues(MaxDelay, nonValidatorDelay)
}

func TestWindowerChangeByHeight(t *testing.T) {
	require := require.New(t)

	subnetID := ids.ID{0, 1}
	chainID := ids.ID{0, 2}
	validatorIDs := make([]ids.NodeID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.NodeID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxWindows)
=======
		GetValidatorSetF: func(uint64, ids.ID) (map[ids.NodeID]uint64, error) {
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
			validators := make(map[ids.NodeID]uint64, MaxWindows)
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.Validator, error) {
			vdrs := make(map[ids.NodeID]*validators.Validator, MaxWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.Validator{
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	expectedDelays1 := []time.Duration{
		2 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		1 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(context.Background(), 1, 0, vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}

	expectedDelays2 := []time.Duration{
		5 * WindowDuration,
		1 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays2 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(context.Background(), 2, 0, vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}
}

func TestWindowerChangeByChain(t *testing.T) {
	require := require.New(t)

	subnetID := ids.ID{0, 1}

	rand.Seed(0)
	chainID0 := ids.ID{}
	_, _ = rand.Read(chainID0[:]) // #nosec G404
	chainID1 := ids.ID{}
	_, _ = rand.Read(chainID1[:]) // #nosec G404

	validatorIDs := make([]ids.NodeID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.NodeID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, MaxWindows)
=======
		GetValidatorSetF: func(uint64, ids.ID) (map[ids.NodeID]uint64, error) {
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
			validators := make(map[ids.NodeID]uint64, MaxWindows)
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
			for _, id := range validatorIDs {
				vdrs[id] = &validators.GetValidatorOutput{
=======
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.Validator, error) {
			vdrs := make(map[ids.NodeID]*validators.Validator, MaxWindows)
			for _, id := range validatorIDs {
				vdrs[id] = &validators.Validator{
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
					NodeID: id,
					Weight: 1,
				}
			}
			return vdrs, nil
		},
	}

	w0 := New(vdrState, subnetID, chainID0)
	w1 := New(vdrState, subnetID, chainID1)

	expectedDelays0 := []time.Duration{
		5 * WindowDuration,
		2 * WindowDuration,
		0 * WindowDuration,
		3 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays0 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w0.Delay(context.Background(), 1, 0, vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}

	expectedDelays1 := []time.Duration{
		0 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w1.Delay(context.Background(), 1, 0, vdrID)
		require.NoError(err)
		require.EqualValues(expectedDelay, validatorDelay)
	}
}

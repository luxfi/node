// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
)

func TestValidatorUptimes(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	// get non-existent uptime
	nodeID := ids.GenerateTestNodeID()
	netID := ids.GenerateTestID()
	_, _, err := state.GetUptime(nodeID, netID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent uptime
	err = state.SetUptime(nodeID, netID, 1, time.Now())
	require.ErrorIs(err, database.ErrNotFound)

	testMetadata := &validatorMetadata{
		UpDuration:  time.Hour,
		lastUpdated: time.Now(),
	}
	// load uptime
	state.LoadValidatorMetadata(nodeID, netID, testMetadata)

	// get uptime
	upDuration, lastUpdated, err := state.GetUptime(nodeID, netID)
	require.NoError(err)
	require.Equal(testMetadata.UpDuration, upDuration)
	require.Equal(testMetadata.lastUpdated, lastUpdated)

	// set uptime
	newUpDuration := testMetadata.UpDuration + 1
	newLastUpdated := testMetadata.lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, netID, newUpDuration, newLastUpdated))

	// get new uptime
	upDuration, lastUpdated, err = state.GetUptime(nodeID, netID)
	require.NoError(err)
	require.Equal(newUpDuration, upDuration)
	require.Equal(newLastUpdated, lastUpdated)

	// load uptime changes uptimes
	newTestMetadata := &validatorMetadata{
		UpDuration:  testMetadata.UpDuration + time.Hour,
		lastUpdated: testMetadata.lastUpdated.Add(time.Hour),
	}
	state.LoadValidatorMetadata(nodeID, netID, newTestMetadata)

	// get new uptime
	upDuration, lastUpdated, err = state.GetUptime(nodeID, netID)
	require.NoError(err)
	require.Equal(newTestMetadata.UpDuration, upDuration)
	require.Equal(newTestMetadata.lastUpdated, lastUpdated)

	// delete uptime
	state.DeleteValidatorMetadata(nodeID, netID)

	// get deleted uptime
	_, _, err = state.GetUptime(nodeID, netID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteValidatorMetadata(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	primaryDB := memdb.New()
	chainDB := memdb.New()

	// write empty uptimes
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB))

	// load uptime
	nodeID := ids.GenerateTestNodeID()
	netID := ids.GenerateTestID()
	testUptimeReward := &validatorMetadata{
		UpDuration:      time.Hour,
		lastUpdated:     time.Now(),
		PotentialReward: 100,
		txID:            ids.GenerateTestID(),
	}
	state.LoadValidatorMetadata(nodeID, netID, testUptimeReward)

	// write state, should not reflect to DB yet
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB))
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.False(chainDB.Has(testUptimeReward.txID[:]))

	// get uptime should still return the loaded value
	upDuration, lastUpdated, err := state.GetUptime(nodeID, netID)
	require.NoError(err)
	require.Equal(testUptimeReward.UpDuration, upDuration)
	require.Equal(testUptimeReward.lastUpdated, lastUpdated)

	// update uptimes
	newUpDuration := testUptimeReward.UpDuration + 1
	newLastUpdated := testUptimeReward.lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, netID, newUpDuration, newLastUpdated))

	// write uptimes, should reflect to net DB
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB))
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.True(chainDB.Has(testUptimeReward.txID[:]))
}

func TestValidatorDelegateeRewards(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	// get non-existent delegatee reward
	nodeID := ids.GenerateTestNodeID()
	netID := ids.GenerateTestID()
	_, err := state.GetDelegateeReward(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent delegatee reward
	err = state.SetDelegateeReward(netID, nodeID, 100000)
	require.ErrorIs(err, database.ErrNotFound)

	testMetadata := &validatorMetadata{
		PotentialDelegateeReward: 100000,
	}
	// load delegatee reward
	state.LoadValidatorMetadata(nodeID, netID, testMetadata)

	// get delegatee reward
	delegateeReward, err := state.GetDelegateeReward(netID, nodeID)
	require.NoError(err)
	require.Equal(testMetadata.PotentialDelegateeReward, delegateeReward)

	// set delegatee reward
	newDelegateeReward := testMetadata.PotentialDelegateeReward + 100000
	require.NoError(state.SetDelegateeReward(netID, nodeID, newDelegateeReward))

	// get new delegatee reward
	delegateeReward, err = state.GetDelegateeReward(netID, nodeID)
	require.NoError(err)
	require.Equal(newDelegateeReward, delegateeReward)

	// load delegatee reward changes
	newTestMetadata := &validatorMetadata{
		PotentialDelegateeReward: testMetadata.PotentialDelegateeReward + 100000,
	}
	state.LoadValidatorMetadata(nodeID, netID, newTestMetadata)

	// get new delegatee reward
	delegateeReward, err = state.GetDelegateeReward(netID, nodeID)
	require.NoError(err)
	require.Equal(newTestMetadata.PotentialDelegateeReward, delegateeReward)

	// delete delegatee reward
	state.DeleteValidatorMetadata(nodeID, netID)

	// get deleted delegatee reward
	_, _, err = state.GetUptime(nodeID, netID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestParseValidatorMetadata(t *testing.T) {
	// full is a fully-populated record round-tripped through the native wire.
	full := &validatorMetadata{
		UpDuration:               6000000,
		LastUpdated:              900000,
		PotentialReward:          100000,
		PotentialDelegateeReward: 20000,
		StakerStartTime:          12345,
	}
	fullBytes, err := marshalValidatorMetadata(full)
	require.NoError(t, err)

	type test struct {
		name    string
		bytes   []byte
		initial *validatorMetadata // caller-supplied defaults before parse
		want    *validatorMetadata
		wantErr bool
	}
	tests := []test{
		{
			// Empty ⇒ nothing persisted; caller's tx-derived defaults are kept,
			// only lastUpdated is derived from LastUpdated.
			name:    "nil keeps defaults",
			bytes:   nil,
			initial: &validatorMetadata{StakerStartTime: 900000, LastUpdated: 900000},
			want: &validatorMetadata{
				StakerStartTime: 900000,
				LastUpdated:     900000,
				lastUpdated:     time.Unix(900000, 0),
			},
		},
		{
			name:    "empty keeps defaults",
			bytes:   []byte{},
			initial: &validatorMetadata{},
			want:    &validatorMetadata{lastUpdated: time.Unix(0, 0)},
		},
		{
			// Full native buffer overwrites every serialized field, including the
			// caller's tx-default StakerStartTime.
			name:    "full native round-trip",
			bytes:   fullBytes,
			initial: &validatorMetadata{StakerStartTime: 999},
			want: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
				StakerStartTime:          12345,
				lastUpdated:              time.Unix(900000, 0),
			},
		},
		{
			name:    "truncated buffer errors",
			bytes:   fullBytes[:len(fullBytes)-1],
			initial: &validatorMetadata{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			metadata := tt.initial
			err := parseValidatorMetadata(tt.bytes, metadata)
			if tt.wantErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(tt.want, metadata)
		})
	}
}

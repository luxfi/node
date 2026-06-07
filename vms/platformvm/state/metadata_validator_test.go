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
	"github.com/luxfi/node/vms/pcodecs"
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
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB, CodecVersion1))

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
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB, CodecVersion1))
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
	require.NoError(state.WriteValidatorMetadata(primaryDB, chainDB, CodecVersion1))
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
	type test struct {
		name        string
		bytes       []byte
		expected    *validatorMetadata
		expectedErr error
	}
	tests := []test{
		{
			name:  "nil",
			bytes: nil,
			expected: &validatorMetadata{
				lastUpdated: time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name:  "nil",
			bytes: []byte{},
			expected: &validatorMetadata{
				lastUpdated: time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name: "potential reward only",
			bytes: []byte{
				// potential reward via database.ParseUInt64 (BE — database layer, not codec)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				PotentialReward: 100000,
				lastUpdated:     time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward",
			bytes: []byte{
				// codec version (LE)
				0x00, 0x00,
				// up duration (LE) — 6000000 = 0x808D5B_00_00_00_00_00
				0x80, 0x8D, 0x5B, 0x00, 0x00, 0x00, 0x00, 0x00,
				// last updated (LE) — 900000 = 0xA0BB0D_00_00_00_00_00
				0xA0, 0xBB, 0x0D, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential reward (LE) — 100000 = 0xA08601_00_00_00_00_00
				0xA0, 0x86, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected: &validatorMetadata{
				UpDuration:      6000000,
				LastUpdated:     900000,
				PotentialReward: 100000,
				lastUpdated:     time.Unix(900000, 0),
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward + potential delegatee reward",
			bytes: []byte{
				// codec version (LE)
				0x00, 0x00,
				// up duration (LE) — 6000000
				0x80, 0x8D, 0x5B, 0x00, 0x00, 0x00, 0x00, 0x00,
				// last updated (LE) — 900000
				0xA0, 0xBB, 0x0D, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential reward (LE) — 100000
				0xA0, 0x86, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential delegatee reward (LE) — 20000 = 0x204E_00_00_00_00_00_00
				0x20, 0x4E, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
				lastUpdated:              time.Unix(900000, 0),
			},
			expectedErr: nil,
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version (LE) — 2 = 0x02 0x00, but reading LE that means we want value 2
				// so encode as 0x02 0x00 (low byte first)
				0x02, 0x00,
				// up duration (LE)
				0x80, 0x8D, 0x5B, 0x00, 0x00, 0x00, 0x00, 0x00,
				// last updated (LE)
				0xA0, 0xBB, 0x0D, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential reward (LE)
				0xA0, 0x86, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential delegatee reward (LE)
				0x20, 0x4E, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected:    nil,
			expectedErr: pcodecs.ErrUnknownVersion,
		},
		{
			name: "short byte len",
			bytes: []byte{
				// codec version (LE)
				0x00, 0x00,
				// up duration (LE)
				0x80, 0x8D, 0x5B, 0x00, 0x00, 0x00, 0x00, 0x00,
				// last updated (LE)
				0xA0, 0xBB, 0x0D, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential reward (LE)
				0xA0, 0x86, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
				// potential delegatee reward (truncated)
				0x20, 0x4E, 0x00, 0x00, 0x00, 0x00,
			},
			expected:    nil,
			expectedErr: pcodecs.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var metadata validatorMetadata
			err := parseValidatorMetadata(tt.bytes, &metadata)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &metadata)
		})
	}
}

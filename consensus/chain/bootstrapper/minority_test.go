// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/set"
)

func TestNewMinority(t *testing.T) {
	minority := NewMinority(
		log.NewNoOpLogger(), // log
		set.Of(nodeID0), // frontierNodes
		2,               // maxOutstanding
	)

	expectedMinority := &Minority{
		requests: requests{
			maxOutstanding: 2,
			pendingSend:    set.Of(nodeID0),
		},
		log: log.NewNoOpLogger(),
	}
	// Compare only the relevant fields, not the logger
	require.Equal(t, expectedMinority.requests, minority.requests)
	require.NotNil(t, minority.log)
}

func TestMinorityGetPeers(t *testing.T) {
	tests := []struct {
		name          string
		minority      Poll
		expectedState Poll
		expectedPeers set.Set[ids.NodeID]
	}{
		{
			name: "max outstanding",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			expectedPeers: nil,
		},
		{
			name: "send until max outstanding",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0, nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0, nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			expectedPeers: set.Of(nodeID0, nodeID1),
		},
		{
			name: "send until no more to send",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Of(nodeID0),
				},
				log: log.NewNoOpLogger(),
			},
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 2,
					pendingSend:    set.Set[ids.NodeID]{},
					outstanding:    set.Of(nodeID0),
				},
				log: log.NewNoOpLogger(),
			},
			expectedPeers: set.Of(nodeID0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			peers := test.minority.GetPeers(context.Background())
			// Compare only the relevant fields, not the logger
			actual := test.minority.(*Minority)
			expected := test.expectedState.(*Minority)
			require.Equal(expected.requests, actual.requests)
			require.Equal(expected.receivedSet, actual.receivedSet)
			require.Equal(expected.received, actual.received)
			require.Equal(test.expectedPeers, peers)
		})
	}
}

func TestMinorityRecordOpinion(t *testing.T) {
	tests := []struct {
		name          string
		minority      Poll
		nodeID        ids.NodeID
		blkIDs        set.Set[ids.ID]
		expectedState Poll
		expectedErr   error
	}{
		{
			name: "unexpected response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			nodeID: nodeID0,
			blkIDs: nil,
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			expectedErr: nil,
		},
		{
			name: "unfinished after response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Of(nodeID1),
				},
				log: log.NewNoOpLogger(),
			},
			nodeID: nodeID1,
			blkIDs: set.Of(blkID0),
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					pendingSend:    set.Of(nodeID0),
					outstanding:    set.Set[ids.NodeID]{},
				},
				log:         nil,
				receivedSet: set.Of(blkID0),
			},
			expectedErr: nil,
		},
		{
			name: "finished after response",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID2),
				},
				log: log.NewNoOpLogger(),
			},
			nodeID: nodeID2,
			blkIDs: set.Of(blkID1),
			expectedState: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Set[ids.NodeID]{},
				},
				log:         log.NewNoOpLogger(),
				receivedSet: set.Of(blkID1),
				received:    []ids.ID{blkID1},
			},
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.minority.RecordOpinion(context.Background(), test.nodeID, test.blkIDs)
			// Compare only the relevant fields, not the logger
			actual := test.minority.(*Minority)
			expected := test.expectedState.(*Minority)
			require.Equal(expected.requests, actual.requests)
			require.Equal(expected.receivedSet, actual.receivedSet)
			require.Equal(expected.received, actual.received)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestMinorityResult(t *testing.T) {
	tests := []struct {
		name              string
		minority          Poll
		expectedAccepted  []ids.ID
		expectedFinalized bool
	}{
		{
			name: "not finalized",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
					outstanding:    set.Of(nodeID1),
				},
				log:      nil,
				received: nil,
			},
			expectedAccepted:  nil,
			expectedFinalized: false,
		},
		{
			name: "finalized",
			minority: &Minority{
				requests: requests{
					maxOutstanding: 1,
				},
				log:         nil,
				receivedSet: set.Of(blkID0),
				received:    []ids.ID{blkID0},
			},
			expectedAccepted:  []ids.ID{blkID0},
			expectedFinalized: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			accepted, finalized := test.minority.Result(context.Background())
			require.Equal(test.expectedAccepted, accepted)
			require.Equal(test.expectedFinalized, finalized)
		})
	}
}

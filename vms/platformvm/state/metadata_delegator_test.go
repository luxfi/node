// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/pcodecs"
)

func TestParseDelegatorMetadata(t *testing.T) {
	type test struct {
		name        string
		bytes       []byte
		expected    *delegatorMetadata
		expectedErr error
	}
	tests := []test{
		{
			name: "potential reward only no codec",
			bytes: []byte{
				// potential reward via database.ParseUInt64 (BE — database layer, not codec)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 0,
			},
			expectedErr: nil,
		},
		{
			name: "potential reward + staker start time with codec v1",
			bytes: []byte{
				// codec version (LE) — 1 = 0x01 0x00
				0x01, 0x00,
				// potential reward (LE) — 123
				0x7b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// staker start time (LE) — 456 = 0xC8_01_00_...
				0xc8, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expectedErr: nil,
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version (LE) — 2 = 0x02 0x00
				0x02, 0x00,
				// potential reward (LE)
				0x7b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// staker start time (LE)
				0xc8, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected:    nil,
			expectedErr: pcodecs.ErrUnknownVersion,
		},
		{
			name: "short byte len",
			bytes: []byte{
				// codec version (LE)
				0x01, 0x00,
				// potential reward (LE)
				0x7b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// staker start time (truncated)
				0xc8, 0x01, 0x00, 0x00, 0x00, 0x00,
			},
			expected:    nil,
			expectedErr: pcodecs.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var metadata delegatorMetadata
			err := parseDelegatorMetadata(tt.bytes, &metadata)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &metadata)
		})
	}
}

func TestWriteDelegatorMetadata(t *testing.T) {
	type test struct {
		name     string
		version  uint16
		metadata *delegatorMetadata
		expected []byte
	}
	tests := []test{
		{
			name:    CodecVersion0Tag,
			version: CodecVersion0,
			metadata: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expected: []byte{
				// potential reward via database.PutUInt64 (BE — database layer, not codec)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
			},
		},
		{
			name:    CodecVersion1Tag,
			version: CodecVersion1,
			metadata: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expected: []byte{
				// codec version (LE)
				0x01, 0x00,
				// potential reward (LE)
				0x7b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// staker start time (LE)
				0xc8, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()
			tt.metadata.txID = ids.GenerateTestID()
			require.NoError(writeDelegatorMetadata(db, tt.metadata, tt.version))
			bytes, err := db.Get(tt.metadata.txID[:])
			require.NoError(err)
			require.Equal(tt.expected, bytes)
		})
	}
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
)

func TestParseDelegatorMetadata(t *testing.T) {
	full := &delegatorMetadata{PotentialReward: 123, StakerStartTime: 456}
	fullBytes, err := marshalDelegatorMetadata(full)
	require.NoError(t, err)

	type test struct {
		name    string
		bytes   []byte
		initial *delegatorMetadata // caller-supplied defaults before parse
		want    *delegatorMetadata
		wantErr bool
	}
	tests := []test{
		{
			// Empty ⇒ nothing persisted; the caller's tx-derived StakerStartTime
			// default is kept.
			name:    "empty keeps defaults",
			bytes:   nil,
			initial: &delegatorMetadata{StakerStartTime: 456},
			want:    &delegatorMetadata{StakerStartTime: 456},
		},
		{
			name:    "full native round-trip",
			bytes:   fullBytes,
			initial: &delegatorMetadata{StakerStartTime: 999},
			want:    &delegatorMetadata{PotentialReward: 123, StakerStartTime: 456},
		},
		{
			name:    "truncated buffer errors",
			bytes:   fullBytes[:len(fullBytes)-1],
			initial: &delegatorMetadata{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			metadata := tt.initial
			err := parseDelegatorMetadata(tt.bytes, metadata)
			if tt.wantErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(tt.want, metadata)
		})
	}
}

func TestWriteDelegatorMetadata(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	metadata := &delegatorMetadata{
		PotentialReward: 123,
		StakerStartTime: 456,
		txID:            ids.GenerateTestID(),
	}
	require.NoError(writeDelegatorMetadata(db, metadata))

	bytes, err := db.Get(metadata.txID[:])
	require.NoError(err)

	// The persisted bytes round-trip back to the same serialized fields.
	got := &delegatorMetadata{txID: metadata.txID}
	require.NoError(parseDelegatorMetadata(bytes, got))
	require.Equal(metadata, got)
}

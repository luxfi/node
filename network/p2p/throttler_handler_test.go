// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/p2p"
)

var _ Handler = (*TestHandler)(nil)

func TestThrottlerHandlerGossip(t *testing.T) {
	tests := []struct {
		name      string
		Throttler Throttler
		expected  bool
	}{
		{
			name:      "not throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
			expected:  true,
		},
		{
			name:      "throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			called := false
			handler := NewThrottlerHandler(
				TestHandler{
					GossipF: func(context.Context, ids.NodeID, []byte) {
						called = true
					},
				},
				tt.Throttler,
				log.NoLog{},
			)

			handler.Gossip(context.Background(), ids.GenerateTestNodeID(), []byte("foobar"))
			require.Equal(tt.expected, called)
		})
	}
}

func TestThrottlerHandlerRequest(t *testing.T) {
	tests := []struct {
		name        string
		Throttler   Throttler
		expectedErr *p2p.Error
	}{
		{
			name:      "not throttled",
			Throttler: NewSlidingWindowThrottler(time.Second, 1),
		},
		{
			name:        "throttled",
			Throttler:   NewSlidingWindowThrottler(time.Second, 0),
			expectedErr: ErrThrottled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := NewThrottlerHandler(
				NoOpHandler{},
				tt.Throttler,
				log.NoLog{},
			)
			_, err := handler.Request(context.Background(), ids.GenerateTestNodeID(), time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

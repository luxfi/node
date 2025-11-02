// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/consensustest"
)

// testStateHolder is a test implementation of state management
type testStateHolder struct {
	value atomic.Value
}

func (s *testStateHolder) Get() interfaces.State {
	if val := s.value.Load(); val != nil {
		return val.(interfaces.State)
	}
	return interfaces.NormalOp
}

func (s *testStateHolder) Set(state interfaces.State) {
	s.value.Store(state)
}

func TestRejectMiddleware(t *testing.T) {
	type test struct {
		name               string
		handler            http.Handler
		state              consensus.State
		expectedStatusCode int
	}

	tests := []test{
		{
			name:               "chain is state syncing",
			state:              consensus.StateSyncing,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name:               "chain is bootstrapping",
			state:              consensus.Bootstrapping,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is done bootstrapping",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
			state:              consensus.NormalOp,
			expectedStatusCode: http.StatusTeapot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Create a test context
			stateHolder := &testStateHolder{}
			stateHolder.Set(tt.state)
			ctx := context.WithValue(context.Background(), "stateHolder", stateHolder)

			middleware := rejectMiddleware(tt.handler, ctx)
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, nil)
			require.Equal(tt.expectedStatusCode, w.Code)
		})
	}
}

func TestHTTPHeaderRouteIsCanonical(t *testing.T) {
	wantHeaderKey := http.CanonicalHeaderKey(HTTPHeaderRoute)
	require.Equal(t, wantHeaderKey, HTTPHeaderRoute)
}

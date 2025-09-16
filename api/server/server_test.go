// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/interfaces"
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
		handlerFunc        func(*require.Assertions) http.Handler
		state              interfaces.State
		expectedStatusCode int
	}

	tests := []test{
		{
			name: "chain is state syncing",
			handlerFunc: func(require *require.Assertions) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					require.Fail("shouldn't have called handler")
				})
			},
			state:              interfaces.StateSyncing,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is bootstrapping",
			handlerFunc: func(require *require.Assertions) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					require.Fail("shouldn't have called handler")
				})
			},
			state:              interfaces.Bootstrapping,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is done bootstrapping",
			handlerFunc: func(*require.Assertions) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				})
			},
			state:              interfaces.NormalOp,
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

			middleware := rejectMiddleware(tt.handlerFunc(require), ctx)
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, nil)
			require.Equal(tt.expectedStatusCode, w.Code)
		})
	}
}

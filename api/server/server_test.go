// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/consensustest"
)

func TestRejectMiddleware(t *testing.T) {
	type test struct {
		name               string
		handlerFunc        func(*require.Assertions) http.Handler
		state              consensus.State
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
			state:              consensus.StateSyncing,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is bootstrapping",
			handlerFunc: func(require *require.Assertions) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					require.Fail("shouldn't have called handler")
				})
			},
			state:              consensus.Bootstrapping,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is done bootstrapping",
			handlerFunc: func(*require.Assertions) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				})
			},
			state:              consensus.NormalOp,
			expectedStatusCode: http.StatusTeapot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			consensusCtx := consensustest.Context(t, consensustest.CChainID)
			ctx := consensustest.ConsensusContext(consensusCtx)
			ctx.State.Set(consensus.EngineState{
				State: tt.state,
			})

			middleware := rejectMiddleware(tt.handlerFunc(require), ctx)
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, nil)
			require.Equal(tt.expectedStatusCode, w.Code)
		})
	}
}

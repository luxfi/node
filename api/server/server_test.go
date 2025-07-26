// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
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

			ctx := consensustest.Context(t, consensustest.CChainID)
			ctx := consensustest.ConsensusContext(ctx)
			ctx.State.Set(consensus.EngineState{
				State: tt.state,
			})

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

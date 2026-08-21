// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
)

// newZChain builds a real Z-Chain the way node/vms.go does: the production
// Factory, then Initialize. Its CreateHandlers map is what the chain manager
// mounts, so the handlers under test are the shipped ones.
func newZChain(t *testing.T) *zkvm.VM {
	t.Helper()
	logger := log.NewNoOpLogger()
	f := &zkvm.Factory{}
	any, err := f.New(logger)
	require.NoError(t, err)
	vm := any.(*zkvm.VM)
	require.NoError(t, vm.Initialize(context.Background(), vmcore.Init{
		Runtime:  &runtime.Runtime{ChainID: ids.GenerateTestID(), NetworkID: 96369, Log: logger},
		DB:       memdb.New(),
		ToEngine: make(chan vmcore.Message, 8),
		Log:      logger,
		Genesis:  []byte(`{"timestamp":0}`),
	}))
	return vm
}

// TestChainHandlerServesItsOwnPaths pins the mount contract: a handler
// returned by a VM's CreateHandlers is reachable at the paths IT names,
// underneath the endpoint the node mounted it at.
//
// Z-Chain is the case that proved it was not: zkvm returns an http.ServeMux
// per endpoint whose patterns are /getStatus, /getBlock, … Mounted as a leaf
// at /v1/bc/<chainID>/rpc, the mux only ever saw the mount path, matched
// nothing, and answered 404 — the chain ran, produced metrics and finalized
// blocks, and served no HTTP at all.
func TestChainHandlerServesItsOwnPaths(t *testing.T) {
	vm := newZChain(t)
	handlers, err := vm.CreateHandlers(context.Background())
	require.NoError(t, err)

	r := newRouter()
	chainID := ids.GenerateTestID().String()
	base := baseURL + "/bc/" + chainID
	for endpoint, h := range handlers {
		require.NoError(t, r.AddRouter(base, endpoint, h))
	}

	for _, path := range []string{"/rpc/getStatus", "/rpc/getUTXOCount", "/proof/getProofStats"} {
		req := httptest.NewRequest(http.MethodGet, base+path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "%s%s: %s", base, path, rec.Body.String())
	}
}

// TestMountedEndpointStillServesItself keeps the VMs that answer at the mount
// point itself (quantumvm, mpcvm, keyvm, the C-Chain EVM: one JSON-RPC handler
// that ignores the path) working exactly as before.
func TestMountedEndpointStillServesItself(t *testing.T) {
	r := newRouter()
	base := baseURL + "/bc/" + ids.GenerateTestID().String()
	var got string
	rpc := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, r.AddRouter(base, "/rpc", rpc))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, base+"/rpc", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, base+"/rpc", got, "the mount path itself must reach the handler unmodified")
}

// TestSiblingEndpointsDoNotShadow guards the node's own APIs, where several
// endpoints share one base: /v1/health, /v1/health/readiness, /v1/health/health,
// /v1/health/liveness are four distinct handlers and each must keep its own.
func TestSiblingEndpointsDoNotShadow(t *testing.T) {
	r := newRouter()
	base := baseURL + "/health"
	mark := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(name))
		})
	}
	for endpoint, name := range map[string]string{
		"":           "root",
		"/readiness": "readiness",
		"/health":    "health",
		"/liveness":  "liveness",
	} {
		require.NoError(t, r.AddRouter(base, endpoint, mark(name)))
	}
	for endpoint, want := range map[string]string{
		"":           "root",
		"/readiness": "readiness",
		"/health":    "health",
		"/liveness":  "liveness",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base+endpoint, nil))
		require.Equal(t, want, rec.Body.String(), "endpoint %q", endpoint)
	}
}

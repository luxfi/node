// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/utils"

	server "github.com/luxfi/node/server/http"
)

// TestNothingIsServedBeforeTheChainCanAnswer.
//
// The P-Chain's typed surface is built from a Service, and a Service reads
// state the VM does not have until it has bootstrapped — so the app is not
// built until the first request that finds the VM running.
//
// That matters beyond the reads. zip raises an MCP door on any app holding a
// typed op and renders tools/list off the registry WITHOUT asking the VM
// anything, so an app built early would advertise thirty-one tools that every
// answer with a 503. Building late means the door does not exist yet, which is
// an honest thing for a chain that cannot answer.
//
// The check is BEFORE once.Do, so a "still bootstrapping" answer is not cached
// for the life of the process.
func TestNothingIsServedBeforeTheChainCanAnswer(t *testing.T) {
	require := require.New(t)

	vm := &VM{bootstrapped: utils.Atomic[bool]{}, log: log.Noop()}
	at := &lazily{vm: vm}

	rec := httptest.NewRecorder()
	at.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/height", nil))
	require.Equal(http.StatusServiceUnavailable, rec.Code)
	require.Contains(rec.Body.String(), "VM still bootstrapping")
	require.Nil(at.app, "the surface was built for a chain that cannot answer")

	// A second refusal leaves the wrapper exactly as unbuilt as the first. That
	// is what the guard sitting BEFORE once.Do buys: a wrapper that ran the
	// build and cached its failure would answer 503 for the life of the
	// process, long after the chain came up.
	rec = httptest.NewRecorder()
	at.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/height", nil))
	require.Equal(http.StatusServiceUnavailable, rec.Code)
	require.Nil(at.app)
	require.NoError(at.err, "a refusal to serve was recorded as a build failure")
}

// TestTheChainServesItsOpsAndNothingElse.
//
// One endpoint, and it is not the base. The router records a prefix mount only
// for a NAMED endpoint, so an app registered at "" is an exact route and owns
// nothing beneath it — which for a zip app means serving no op, no document and
// no MCP door. The base is now unserved: the JSON-RPC surface that answered
// there is gone, along with the reflection that found it.
func TestTheChainServesItsOpsAndNothingElse(t *testing.T) {
	require := require.New(t)

	handlers, err := (&VM{}).CreateHandlers(context.Background())
	require.NoError(err)
	require.Len(handlers, 1)
	require.Contains(handlers, server.Ops)
	require.IsType(&lazily{}, handlers["/ops"])
}

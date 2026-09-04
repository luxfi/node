// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	"github.com/luxfi/constants"
)

// A chain is named by one alias however a caller capitalises it, and the
// endpoint its RPC sits at is theirs to omit.
//
// The measurement that prompted this: a running node answered
// [Chain]("", "C")+"/rpc" and answered NOTHING for the same address written in
// lower case, or written without the endpoint. Both name the same chain. A
// client that lowercases its URLs, or that drops an endpoint a directory
// listing once told it to write, is not asking for a favour.
//
// Asserted with a handler that reports the path it was reached at, so a 200
// that arrived at the WRONG place cannot pass as a 200 that arrived at the
// right one. Paths are built with [Chain] rather than spelled, which is what
// TestChainAddressBuiltOnlyHere requires of every file in the module.
func TestChainAliasFoldsAndEndpointIsOptional(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t)

	seen := make(chan string, 16)
	spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	// Registered ONCE, the way the chain manager registers a C-Chain: upper
	// case, with the RPC at its own endpoint.
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/C", rpcEndpoint))
	registered := Chain("", "C") + rpcEndpoint

	for _, asked := range []string{
		Chain("", "C") + rpcEndpoint,
		Chain("", "c") + rpcEndpoint,
		Chain("", "C"),
		Chain("", "c"),
	} {
		_, code := ask(t, serve, asked)
		require.Equal(http.StatusOK, code, asked)
		require.Equal(registered, <-seen, "asked %s", asked)
	}

	// An alias longer than a letter folds the same way — the fold is one rule,
	// not a table of the aliases someone remembered.
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/Zoo", rpcEndpoint))
	for _, alias := range []string{"Zoo", "zoo", "ZOO", "zOo"} {
		_, code := ask(t, serve, Chain("", alias))
		require.Equal(http.StatusOK, code, alias)
		require.Equal(Chain("", "Zoo")+rpcEndpoint, <-seen, "asked %s", alias)
	}
}

// The fold reaches a MOUNTED VM's own paths, not only the mount itself.
//
// A VM names what it serves relative to where it was mounted, so the rewritten
// request has to go through the same "route, then the endpoint it lives under"
// cascade an exactly-spelled one does. Folding only the exact match would leave
// every chain's sub-path answering in one case and 404ing in the other.
func TestAFoldedRequestReachesTheMountedVM(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t)

	app := zip.New(zip.Config{AppName: "coreth"})
	zip.Get(app, "/height", func(context.Context, *struct{}) (*height, error) {
		return &height{Height: 7}, nil
	})
	handler, err := Mount(app)
	require.NoError(err)
	t.Cleanup(func() { _ = app.Shutdown() })
	require.NoError(s.AddRoute(handler, constants.ChainAliasPrefix+"/C", rpcEndpoint))

	for _, asked := range []string{
		Chain("", "C") + rpcEndpoint + "/height",
		Chain("", "c") + rpcEndpoint + "/height",
	} {
		body, code := ask(t, serve, asked)
		require.Equal(http.StatusOK, code, "%s: %s", asked, body)
		require.JSONEq(`{"height":7}`, body)
	}
}

// Folding a case is not opening a door. A chain this node does not serve is
// still absent however it is spelled, and the words AROUND the alias are
// literals — only the alias is the caller's to spell.
func TestFoldingAnAliasAddsNoRoute(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t)

	spy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/C", rpcEndpoint))

	for _, dead := range []string{
		// A chain nobody registered, in either case.
		Chain("", "zzz") + rpcEndpoint,
		Chain("", "ZZZ"),
		// An empty alias is not an alias.
		Chain("", ""),
		// An endpoint this chain does not serve.
		Chain("", "c") + "/nowhere",
		Chain("", "C") + "/nowhere",
		// The prefix itself, upper-cased: a different path, not a different
		// capitalisation of this one.
		strings.ToUpper(chainPrefix) + "c" + rpcEndpoint,
	} {
		body, code := ask(t, serve, dead)
		require.Equal(http.StatusNotFound, code, "%s answers: %s", dead, body)
	}
}

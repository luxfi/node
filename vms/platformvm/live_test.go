// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	"github.com/luxfi/ids"
	server "github.com/luxfi/node/server/http"
)

// The projections are only worth what a real request gets back, so this serves
// the P-Chain's app on the node's own mount — the same [server.Mount] the router
// uses, with the same Authorizer installed — and calls it over HTTP.
//
// The handlers here reach a nil VM, so a read that touches state panics rather
// than answering; what is under test is everything BEFORE the handler, which is
// the whole of what changed: the address, the verb, the authorization decision,
// the binding of a URL onto a typed input, and the document and MCP door zip
// raises beside them.
func serving(t *testing.T) http.Handler {
	t.Helper()
	app := (&Service{}).ops(nil)
	t.Cleanup(func() { _ = app.Shutdown() })
	at, err := server.Mount(app)
	require.NoError(t, err)
	return at
}

func get(t *testing.T, at http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.9:41000" // NOT this machine: an ordinary caller
	at.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestTheDocumentIsServedBesideTheOps: zip installs the document, the docs page
// and the MCP door on the app itself, so they live UNDER the mount — which is
// why the ops mount at /ops and not at the chain base, where the router would
// give the app one exact route and nothing beneath it.
func TestTheDocumentIsServedBesideTheOps(t *testing.T) {
	at := serving(t)

	code, body := get(t, at, "/.well-known/openapi.json")
	require.Equal(t, http.StatusOK, code)

	var spec struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &spec))
	require.Equal(t, "Lux P-Chain", spec.Info.Title)
	require.Contains(t, spec.Paths, "/height")
	require.Contains(t, spec.Paths, "/validators")
	require.Contains(t, spec.Paths["/tx"], "get")
	require.Contains(t, spec.Paths["/tx"], "post")
}

// TestAnAgentCanListTheChainsTools reaches the MCP door the same way an agent
// does, and gets the registrations back as tools.
func TestAnAgentCanListTheChainsTools(t *testing.T) {
	at := serving(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		jsonBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:41000"
	at.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var reply struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reply))

	named := map[string]string{}
	for _, tool := range reply.Result.Tools {
		named[tool.Name] = tool.Description
		require.Regexp(t, `^[a-zA-Z0-9_-]{1,64}$`, tool.Name)
	}
	require.Len(t, named, 31)
	require.Equal(t, "Returns the height of the last accepted block.", named["get_height"])
	require.Contains(t, named, "post_tx")
}

// TestAnOrdinaryCallerMayRead: a read is a GET and a GET answers anyone. The
// caller here is not on this machine, which is the only thing the node's
// authorizer asks about — and it is asked at the op-invoke seam, so this holds
// for the MCP door as well as the route.
//
// A nil VM means the handler itself cannot answer, so what is asserted is that
// the request REACHED it: a 403 would have been the authorizer's, and it is the
// answer every one of these reads would give on mainnet if it were a POST.
func TestAnOrdinaryCallerMayRead(t *testing.T) {
	at := serving(t)
	for _, path := range []string{
		"/height", "/timestamp", "/fee/config",
		"/validators?netID=" + ids.Empty.String(),
		"/tx?txID=" + ids.Empty.String() + "&encoding=json",
	} {
		func() {
			defer func() { _ = recover() }() // the nil VM, not the surface
			code, body := get(t, at, path)
			require.NotEqual(t, http.StatusForbidden, code,
				"%s refused an ordinary caller: %s", path, body)
		}()
	}
}

// TestTheURLIsTheWholeInput. A GET carries no body, so every argument a read
// takes has to arrive in its URL — and until the binder could read them, an id
// arrived as the zero id and a list as nil, WITH A 200 ON THE ANSWER.
//
// The input is the P-Chain's own, registered at the P-Chain's own address, and
// reached through the node's own mount: net/http request, fasthttp transport,
// zip's binder, typed input. Only the handler is different, because a handler
// that reads state cannot say what arrived.
func TestTheURLIsTheWholeInput(t *testing.T) {
	var got *GetCurrentValidatorsArgs

	app := zip.New(zip.Config{AppName: "platform", DisableStartupMessage: true})
	zip.Get(app, "/validators", func(_ context.Context, in *GetCurrentValidatorsArgs) (*GetCurrentValidatorsReply, error) {
		got = in
		return &GetCurrentValidatorsReply{}, nil
	})
	t.Cleanup(func() { _ = app.Shutdown() })
	at, err := server.Mount(app)
	require.NoError(t, err)

	netID := ids.GenerateTestID()
	one, two := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

	code, body := get(t, at, fmt.Sprintf("/validators?netID=%s&nodeIDs=%s,%s", netID, one, two))
	require.Equal(t, http.StatusOK, code, body)

	require.NotNil(t, got)
	require.Equal(t, netID, got.ChainID, "the net id did not arrive")
	require.Equal(t, []ids.NodeID{one, two}, got.NodeIDs, "the node ids did not arrive")
}

// jsonBody is a request body from a literal.
func jsonBody(s string) io.Reader { return strings.NewReader(s) }

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
)

// The X-Chain's surface, pinned. Every projection below is a reading of this
// one registry, so the list here is the whole contract: an op that is not on it
// is not served, documented, offered as a tool, or reachable by name.
//
// The NAMES are derived from method and path — never declared — so this table
// is also what holds the addresses still. Renaming a path renames the tool, the
// command and the generated client's method, which is exactly why the addresses
// are pinned rather than described.
var surface = map[string]string{
	"get_height":          "GET /height",
	"get_block":           "GET /block",
	"get_block_by_height": "GET /block/:height",
	"get_tx":              "GET /tx",
	"get_txs":             "GET /txs",
	"get_utxos":           "GET /utxos",
	"get_asset":           "GET /asset",
	"get_balance":         "GET /balance",
	"get_balances":        "GET /balances",
	"post_tx":             "POST /tx",
}

func chainOps(t *testing.T) *zip.App {
	t.Helper()
	app := (&Service{}).ops(log.Noop())
	t.Cleanup(func() { _ = app.Shutdown() })
	return app
}

func TestTheChainRegistersItsSurfaceAndNothingElse(t *testing.T) {
	require := require.New(t)

	got := map[string]string{}
	for _, op := range chainOps(t).Registry() {
		got[zip.ID(op.Method, op.Path)] = op.Method + " " + op.Path
	}
	require.Equal(surface, got)

	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("%d ops: %v", len(names), names)
}

// Every read answers anyone and the one write answers anyone, for two different
// reasons — and both reasons are READ OFF the registration rather than declared
// beside it. A read is a GET, so it changes nothing. The write is at
// [server.Relay], so its bytes arrived already signed.
//
// This is the test that would catch the mistake worth catching: an op registered
// as a POST somewhere other than /tx silently becomes operator-only, and a
// caller on mainnet meets a 403 where a reply used to be.
func TestEveryOpAnswersWhoeverAsks(t *testing.T) {
	for _, op := range chainOps(t).Registry() {
		require.Truef(t, server.Open(zip.Op{Method: op.Method, Path: op.Path}),
			"%s %s answers only the operator; a chain read must answer anyone, and a write must be at %s",
			op.Method, op.Path, server.Relay)
	}
}

// The OpenAPI document, shown. It is what docs.lux.network and every generated
// client are built from, so a summary missing here is a method undescribed
// everywhere.
func TestTheDocumentDescribesEveryOp(t *testing.T) {
	require := require.New(t)
	app := chainOps(t)

	raw, err := json.MarshalIndent(app.OpenAPISpec(), "", "  ")
	require.NoError(err)
	t.Logf("OpenAPI document (%d bytes):\n%s", len(raw), raw)

	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string           `json:"operationId"`
			Description string           `json:"description"`
			Parameters  []map[string]any `json:"parameters"`
		} `json:"paths"`
	}
	require.NoError(json.Unmarshal(raw, &doc))

	seen := map[string]bool{}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			seen[op.OperationID] = true
			require.NotEmptyf(op.Description, "%s %s has no description; the doc comment never reached the document", method, path)
		}
	}
	for name := range surface {
		require.Truef(seen[name], "%s is registered but absent from the document", name)
	}

	// The id a caller writes in the URL is published as a parameter, because the
	// binder fills it. Silence there is what the document used to keep about
	// every id-addressed read.
	block := doc.Paths["/block"]["get"]
	var named []string
	for _, p := range block.Parameters {
		named = append(named, p["name"].(string))
	}
	require.Contains(named, "blockID")
	require.Contains(named, "encoding")

	// And the UTXO read publishes the whole of what it takes: several addresses,
	// and a cursor whose leaves are named through it.
	utxos := doc.Paths["/utxos"]["get"]
	named = nil
	for _, p := range utxos.Parameters {
		named = append(named, p["name"].(string))
	}
	require.Contains(named, "addresses")
	require.Contains(named, "startIndex.address")
	require.Contains(named, "startIndex.utxo")
}

// The MCP tool list, shown. A tool name that fails the client's own pattern is
// a tool no client will call, so the pattern is asserted rather than admired.
func TestTheToolListNamesEveryOp(t *testing.T) {
	require := require.New(t)
	app := chainOps(t)
	require.NoError(app.Build())

	tools := app.MCPTools()
	raw, err := json.MarshalIndent(tools, "", "  ")
	require.NoError(err)
	t.Logf("MCP tools/list (%d tools):\n%s", len(tools), raw)

	var listed []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	require.NoError(json.Unmarshal(raw, &listed))

	seen := map[string]bool{}
	for _, tool := range listed {
		seen[tool.Name] = true
		require.NotEmptyf(tool.Description, "tool %s has no description", tool.Name)
		require.Regexpf(`^[a-zA-Z0-9_-]{1,64}$`, tool.Name,
			"tool %s is not a name an MCP client will accept", tool.Name)
	}
	for name := range surface {
		require.Truef(seen[name], "%s is registered but absent from the tool list", name)
	}
}

// The ZAP schema, shown. It is the wire anything that is not this process reads,
// and its gaps are part of the answer: an op named there is NOT in the text.
func TestTheSchemaStatesTheChainsWire(t *testing.T) {
	schema := zip.ZAPSchema("xvm", chainOps(t))
	t.Logf(".zap schema (%d ops, %d blocked):\n%s", schema.Ops(), schema.Blocked(), schema)
	for _, gap := range schema.Gaps {
		t.Logf("  no layout: %s", gap)
	}
}

// The end-to-end proof, and the only one that counts: a real app on a real unix
// socket, the op addressed BY NAME over the op-call plane, the input and the
// answer crossing as ZAP through the kernel — and an ids.ID coming back the same
// 32 bytes it went out as.
//
// The handler is the one the chain registers. What is not exercised is the VM
// underneath it, which is why the op chosen is the one that reads its whole
// answer out of its input.
func TestAnOpAnswersOverThePlane(t *testing.T) {
	require := require.New(t)

	id := ids.ID{0: 0xf0, 15: 0x5a, 31: 0x0d}
	app := zip.New(zip.Config{AppName: "xvm", DisableStartupMessage: true})
	zip.Get(app, "/tx", func(_ context.Context, in *apitypes.GetTxArgs) (*apitypes.GetTxReply, error) {
		return &apitypes.GetTxReply{Tx: json.RawMessage(`"0xdead"`), Encoding: in.Encoding}, nil
	}, zip.WithOperationID("get_tx"))

	// A short path on purpose: a unix socket address is capped near 104 bytes
	// and the per-test temp dir is longer than that on macOS.
	dir, mkerr := os.MkdirTemp("/tmp", "xvm")
	require.NoError(mkerr)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	go func() { _ = app.Listen(sock) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	listening(t, sock)

	c, err := zip.Dial(sock)
	require.NoError(err)
	t.Cleanup(func() { _ = c.Close() })

	in := apitypes.GetTxArgs{TxID: id, Encoding: formatting.Hex}
	out, err := zip.Call[apitypes.GetTxArgs, apitypes.GetTxReply](context.Background(), c, "get_tx", &in)
	require.NoError(err)
	require.Equal(formatting.Hex, out.Encoding)
	t.Logf("get_tx over the plane: sent txID=%s, answered encoding=%s tx=%s", id, out.Encoding, out.Tx)
}

func listening(t *testing.T, sock string) {
	t.Helper()
	for range 200 {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("%s never began listening: %v", sock, err)
	}
	t.Fatalf("%s never began listening", sock)
}

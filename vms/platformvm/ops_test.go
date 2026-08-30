// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
)

// surface is the P-Chain's registry, built without a VM. Every projection below
// is read off it, which is the point: the registration is the one source, and
// the document, the tools, the schema and the routes are its readings.
func surface(t *testing.T) *zip.App {
	t.Helper()
	return (&Service{}).ops(nil)
}

// TestEveryOpAnswersWhoeverAsks holds the authorization rule at the place it is
// decided — the registration — rather than at the place it is enforced.
//
// [server.Open] reads a tier off the verb: GET and HEAD change nothing and
// answer anyone, [server.Relay] takes bytes that carry their own signature, and
// everything else answers only the node's operator. So an op registered as a
// POST anywhere but /tx becomes operator-only WITHOUT ANYONE SAYING SO, and on
// mainnet that is a 403 where a reply used to be. Every one of these reads is
// public today.
func TestEveryOpAnswersWhoeverAsks(t *testing.T) {
	for _, route := range surface(t).Routes() {
		op := zip.Op{Method: route.Method, Path: route.Pattern}
		if server.Open(op) {
			continue
		}
		t.Errorf("%s %s answers only the operator; a read must be a GET and the one write must be %s",
			route.Method, route.Pattern, server.Relay)
	}
}

// TestTheWriteIsTheRelay is the other half: the P-Chain has exactly one
// operation that changes anything, and it is at the one address the node opens
// to unauthenticated writes. A second write registered anywhere would be caught
// by the test above; a second write registered AT the relay would not, so it is
// counted here.
func TestTheWriteIsTheRelay(t *testing.T) {
	var writes []string
	for _, route := range surface(t).Routes() {
		if route.Method != http.MethodGet && route.Method != http.MethodHead {
			writes = append(writes, route.Method+" "+route.Pattern)
		}
	}
	require.Equal(t, []string{http.MethodPost + " " + server.Relay}, writes)
}

// TestTheDocumentIsTheRegistration reads the OpenAPI document the way every
// consumer does — through JSON — and checks that what it says about an
// operation came from the code rather than from a second description of it.
func TestTheDocumentIsTheRegistration(t *testing.T) {
	require := require.New(t)

	raw, err := json.Marshal(surface(t).OpenAPISpec())
	require.NoError(err)

	var spec struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Description string `json:"description"`
			Parameters  []struct {
				Name   string `json:"name"`
				In     string `json:"in"`
				Schema struct {
					Type  string `json:"type"`
					Items struct {
						Type string `json:"type"`
					} `json:"items"`
				} `json:"schema"`
			} `json:"parameters"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	require.NoError(json.Unmarshal(raw, &spec))
	require.Equal("Lux P-Chain", spec.Info.Title)

	// The name is DERIVED from the address. Nothing declares it, so the two
	// cannot drift, and no name carries a dot or a capital — an MCP client
	// refuses those.
	for path, methods := range spec.Paths {
		for method, op := range methods {
			require.Equal(zip.ID(method, path), op.OperationID)
			require.Regexp(`^[a-zA-Z0-9_-]{1,64}$`, op.OperationID)
			require.NotEmpty(op.Description, "%s %s has no description", method, path)
		}
	}

	// The description is the handler's doc comment, reached through zipdoc.
	require.Equal(
		"Returns the height of the last accepted block.",
		spec.Paths["/height"]["get"].Description,
	)

	// A reply is described by its Go type.
	require.Equal(
		"#/components/schemas/GetHeightResponse",
		spec.Paths["/height"]["get"].Responses["200"].Content["application/json"].Schema.Ref,
	)

	// And an argument is described by the field it binds to — an id is a string
	// on the wire whatever it is made of, and a repeated argument is an array.
	kind := map[string]string{}
	for _, p := range spec.Paths["/validators"]["get"].Parameters {
		if p.In != "query" {
			continue
		}
		kind[p.Name] = p.Schema.Type
		if p.Schema.Type == "array" {
			kind[p.Name] += " of " + p.Schema.Items.Type
		}
	}
	require.Equal("string", kind["netID"])
	require.Equal("array of string", kind["nodeIDs"])
}

// TestEveryOpIsATool: zip raises an MCP door on any app holding a typed op, and
// the tools there are the same registrations. An agent's view of the P-Chain is
// therefore the P-Chain, not a hand-kept list beside it.
func TestEveryOpIsATool(t *testing.T) {
	require := require.New(t)
	app := surface(t)

	tools := app.MCPTools()
	require.Len(tools, len(app.Routes()))

	byName := map[string]string{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		desc, _ := tool["description"].(string)
		byName[name] = desc
	}
	require.Contains(byName, "get_height")
	require.Contains(byName, "post_tx")
	require.Equal("Returns the height of the last accepted block.", byName["get_height"])
}

// TestEveryMessageCrossesThePlane: a field on the ZAP plane is an offset and a
// width, so a type with no layout — a map, an interface, a fixed array that is
// not bytes — cannot cross at all. Every P-Chain message states one.
//
// Gaps is what to read here. It names the ops that are NOT in the schema text,
// which is the question; the coded ledger answers a different one (which fields
// need a generated codec) and a type that already has one still appears there.
func TestEveryMessageCrossesThePlane(t *testing.T) {
	require := require.New(t)
	schema := zip.ZAPSchema("platform", surface(t))

	require.Empty(schema.Gaps, "these ops cannot cross the plane: %+v", schema.Gaps)
	require.Zero(schema.Blocked())
	require.Equal(len(surface(t).Routes()), schema.Ops())
	require.Empty(schema.Dropped, "these fields cross without their value: %+v", schema.Dropped)

	idl := schema.String()
	require.Contains(idl, "package platform")
	require.Contains(idl, "interface platform")

	if out := os.Getenv("ZAP_SCHEMA_OUT"); out != "" {
		require.NoError(os.WriteFile(filepath.Join(out, "platform.zap"), []byte(idl), 0o644))
	}
}

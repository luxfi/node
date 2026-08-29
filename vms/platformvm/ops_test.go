// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpsProjection asserts that registering platform.getHeight as a typed op
// is what puts it in the OpenAPI document: path, method, operation id, the
// reply schema read off the Go type, and the description read off the
// handler's doc comment by zipdoc. The registration is the one source — when
// this drifts, so do the docs site, the SDKs, the MCP tools and the CLI.
//
// Read through JSON because that is how every consumer reads it.
func TestOpsProjection(t *testing.T) {
	require := require.New(t)

	raw, err := json.Marshal(ops(nil).OpenAPISpec())
	require.NoError(err)

	var spec struct {
		Paths map[string]struct {
			Get struct {
				OperationID string `json:"operationId"`
				Summary     string `json:"summary"`
				Description string `json:"description"`
				Tags        []string
				Responses   map[string]struct {
					Content map[string]struct {
						Schema struct {
							Ref string `json:"$ref"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			} `json:"get"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(json.Unmarshal(raw, &spec))

	require.Contains(spec.Paths, "/height")
	get := spec.Paths["/height"].Get
	require.Equal("platform.getHeight", get.OperationID)
	require.Equal("Height of the last accepted block", get.Summary)
	require.Equal("Returns the height of the last accepted block.", get.Description)
	require.Equal([]string{"platform"}, get.Tags)

	require.Contains(get.Responses, "200")
	require.Equal(
		"#/components/schemas/GetHeightResponse",
		get.Responses["200"].Content["application/json"].Schema.Ref,
	)

	require.Contains(spec.Components.Schemas, "GetHeightResponse")
	reply := spec.Components.Schemas["GetHeightResponse"]
	require.Equal("object", reply.Type)
	require.Contains(reply.Properties, "height")
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The P-Chain API as typed ops.
//
// A JSON-RPC method is described nowhere: the name is a string gorilla/rpc
// finds by reflection, mapped from platform.getHeight to Service.GetHeight by a
// codec, and the reply is whatever the last edit made it. Nothing reads that,
// so the SDK and the CLI carry a hand-written copy of the same shape and drift
// from it in silence. A typed op is described by its own Go types, once, and
// the OpenAPI document, the docs page, the MCP tools and the CLI are all read
// off the registration below.
//
// An op REPLACES its method. getHeight is not an exported method on Service any
// more, so gorilla does not serve it and cannot: there is one address for it
// and it is this one. The methods still on gorilla keep answering at the chain
// base on the same listener — see [VM.CreateHandlers].

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

package platformvm

import (
	"net/http"

	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/version"
)

// ops is the P-Chain's typed surface.
func ops(s *Service) *zip.App {
	app := zip.New(zip.Config{
		AppName: "platform",
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux P-Chain",
			Description: "The Lux primary network's platform chain: validators, stake, and the chains it creates.",
			Version:     version.Current.String(),
		},
	})

	zip.Get(app, "/height", s.height,
		zip.WithOperationID("platform.getHeight"),
		zip.WithSummary("Height of the last accepted block"),
		zip.WithTags("platform"),
	)

	return app
}

// mount serves the typed surface and hands back its net/http face, which is
// what the node's router takes.
func mount(s *Service) (http.Handler, error) { return server.Mount(ops(s)) }

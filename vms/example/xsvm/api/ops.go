// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// ops is what this chain answers, stated once. Each registration carries its
// own In and Out, and the route, the OpenAPI document and the generated client
// all come from that one entry.
//
// A READ is a GET, because a read changes nothing. The one WRITE is at
// [server.Relay], where the bytes arrive already signed and carry their own
// authority. A path names the thing, not the verb.
func (s *Server) Ops(logger log.Logger) *zip.App {
	app := zip.New(zip.Config{
		AppName:               "xsvm",
		Logger:                logger,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux xsvm",
			Description: "The example chain: what an address holds, what it has sent, and the blocks that recorded it.",
		},
	})

	// The chain itself.
	zip.Get(app, "/network", s.getNetwork)
	zip.Get(app, "/genesis", s.getGenesis)

	// What an address holds, and what it has spent.
	zip.Get(app, "/nonce", s.getNonce)
	zip.Get(app, "/balance", s.getBalance)
	zip.Get(app, "/loan", s.getLoan)

	// Blocks.
	zip.Get(app, "/block", s.getBlock)
	zip.Get(app, "/block/last", s.getLastAccepted)

	// Transactions, and what an export produced.
	zip.Post(app, server.Relay, s.issueTx)
	zip.Get(app, "/message", s.getMessage)

	return app
}

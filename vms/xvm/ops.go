// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// ops is what the X-Chain answers, stated once. Each registration carries its
// own In and Out, and from that one entry come the route, the OpenAPI document,
// the MCP tool, the CLI command, the generated client and the op-call plane —
// so the chain's surface is written in one place and read in eight.
//
// A READ is a GET, because a read changes nothing and therefore has nothing to
// authorize; that is what lets anyone make one (see [server.Authorize]). The one
// WRITE is at [server.Relay], and it is open for a different reason: the bytes
// arrive already signed, so they carry their own authority and the node has none
// to lend — it holds no key that could have signed them.
//
// A path names the thing, not the verb: /block is a block and /tx is a
// transaction, so reading one and issuing one are the same address under two
// methods. The operation NAMES are derived from that (get_block, get_tx,
// post_tx) and never declared, so the address and the name cannot drift.
func (s *Service) ops(logger log.Logger) *zip.App {
	app := zip.New(zip.Config{
		AppName:               "xvm",
		Logger:                logger,
		DisableStartupMessage: true,
	})

	zip.Get(app, "/height", s.getHeight)
	zip.Get(app, "/block", s.getBlock)
	zip.Get(app, "/block/:height", s.getBlockByHeight)
	zip.Get(app, "/tx", s.getTx)
	zip.Get(app, "/txs", s.getAddressTxs)
	zip.Get(app, "/utxos", s.getUTXOs)
	zip.Get(app, "/asset", s.getAsset)
	zip.Get(app, "/balance", s.getBalance)
	zip.Get(app, "/balances", s.getBalances)

	zip.Post(app, server.Relay, s.issueTx)

	return app
}

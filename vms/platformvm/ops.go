// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/version"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// ops is what the P-Chain answers, stated once. Each registration carries its
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
// A path names the thing, not the verb: /tx is a transaction, so reading one and
// issuing one are the same address under two methods. The operation NAMES are
// derived from that and never declared, so the address and the name cannot
// drift, and the mount is the namespace, so two chains cannot collide.
//
// A GET carries no body, so a read's whole input is its URL, and every argument
// below rides one: an id and a node id read themselves from text, a list is
// written with commas, a cursor's leaves are named through it, and a height
// spells its reserved value "proposed".
func (s *Service) ops(logger log.Logger) *zip.App {
	app := zip.New(zip.Config{
		AppName:               "platform",
		Logger:                logger,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux P-Chain",
			Description: "The Lux primary network's platform chain: validators, stake, and the chains it creates.",
			Version:     version.Current.String(),
		},
	})

	// The chain itself.
	zip.Get(app, "/height", s.getHeight)
	zip.Get(app, "/height/proposed", s.getProposedHeight)
	zip.Get(app, "/timestamp", s.getTimestamp)
	zip.Get(app, "/supply", s.getCurrentSupply)

	// What an address holds.
	zip.Get(app, "/balance", s.getBalance)
	zip.Get(app, "/utxos", s.getUTXOs)

	// Stake.
	zip.Get(app, "/stake", s.getStake)
	zip.Get(app, "/stake/min", s.getMinStake)
	zip.Get(app, "/stake/total", s.getTotalStake)
	zip.Get(app, "/stake/asset", s.getStakingAssetID)

	// The nets, and the chains they validate.
	zip.Get(app, "/net", s.getNet)
	zip.Get(app, "/nets", s.getNets)
	zip.Get(app, "/net/blockchains", s.validates)
	zip.Get(app, "/chains", s.getChains)
	zip.Get(app, "/blockchains", s.getBlockchains)
	zip.Get(app, "/blockchain/status", s.getBlockchainStatus)
	zip.Get(app, "/blockchain/net", s.validatedBy)

	// Validators.
	zip.Get(app, "/validators", s.getCurrentValidators)
	zip.Get(app, "/validators/at", s.getValidatorsAt)
	zip.Get(app, "/validators/all", s.getAllValidatorsAt)
	zip.Get(app, "/validator", s.getL1Validator)

	// Transactions.
	zip.Get(app, "/tx", s.getTx)
	zip.Get(app, "/tx/status", s.getTxStatus)
	zip.Get(app, "/tx/rewards", s.getRewardUTXOs)
	zip.Post(app, server.Relay, s.issueTx)

	// Blocks.
	zip.Get(app, "/block", s.getBlock)
	zip.Get(app, "/block/:height", s.getBlockByHeight)

	// Fees.
	zip.Get(app, "/fee", s.getFeeState)
	zip.Get(app, "/fee/config", s.getFeeConfig)
	zip.Get(app, "/fee/validator", s.getValidatorFeeState)
	zip.Get(app, "/fee/validator/config", s.getValidatorFeeConfig)

	return app
}

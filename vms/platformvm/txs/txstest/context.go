// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"time"

	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/wallet/chain/p/builder"
)

func newContext(
	ctx *consensusctx.Context,
	networkID uint32,
	luxAssetID ids.ID,
	cfg *config.Config,
	timestamp time.Time,
) *builder.Context {
	builderContext := &builder.Context{
		NetworkID: networkID,
		XAssetID:  luxAssetID,
	}

	// For test purposes, use default values
	// Complexity weights and gas price would be set here if needed

	return builderContext
}

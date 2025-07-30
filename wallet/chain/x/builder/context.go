// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/utils/constants"
	log "github.com/luxfi/log"
)

const Alias = "X"

type Context struct {
	NetworkID        uint32
	BlockchainID     ids.ID
	LUXAssetID       ids.ID
	BaseTxFee        uint64
	CreateAssetTxFee uint64
}

func NewLinearContext(
	networkID uint32,
	blockchainID ids.ID,
	luxAssetID ids.ID,
) (*consensus.Context, error) {
	lookup := ids.NewAliaser()
	return &consensus.Context{
		NetworkID:  networkID,
		SubnetID:   constants.PrimaryNetworkID,
		ChainID:    blockchainID,
		LUXAssetID: luxAssetID,
		Log:        log.NewNoOpLogger(),
		BCLookup:   lookup,
	}, lookup.Alias(blockchainID, Alias)
}

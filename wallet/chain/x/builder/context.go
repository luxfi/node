// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
)

const Alias = "X"

type Context struct {
	NetworkID        uint32
	BlockchainID     ids.ID
	LUXAssetID       ids.ID
	BaseTxFee        uint64
	CreateAssetTxFee uint64
}

func NewConsensusContext(
	networkID uint32,
	blockchainID ids.ID,
	luxAssetID ids.ID,
) (*consensus.Context, error) {
	lookup := ids.NewAliaser()
	return &consensus.Context{
		NetworkID:  networkID,
		SubnetID:   constants.PrimaryNetworkID,
		ChainID:    blockchainID,
		XChainID:   blockchainID,
		LUXAssetID: luxAssetID,
		Log:        logging.NoLog{},
		BCLookup:   lookup,
	}, lookup.Alias(blockchainID, Alias)
}

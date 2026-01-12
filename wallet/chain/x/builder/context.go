// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/luxfi/consensus/runtime"
	"github.com/luxfi/ids"
)

const Alias = "X"

type Context struct {
	NetworkID        uint32
	BlockchainID     ids.ID
	XAssetID         ids.ID
	BaseTxFee        uint64
	CreateAssetTxFee uint64
}

func NewConsensusContext(
	networkID uint32,
	blockchainID ids.ID,
	luxAssetID ids.ID,
) (*runtime.Runtime, error) {
	lookup := ids.NewAliaser()
	ctx := &runtime.Runtime{
		NetworkID: networkID,
		ChainID:   blockchainID,
		XChainID:  blockchainID,
		XAssetID:  luxAssetID,
	}
	return ctx, lookup.Alias(blockchainID, Alias)
}

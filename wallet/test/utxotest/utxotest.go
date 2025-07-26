// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxotest

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/wallet"
)

func NewDeterministicChainUTXOs(t *testing.T, utxoSets map[ids.ID][]*lux.UTXO) *DeterministicChainUTXOs {
	globalUTXOs := wallet.NewUTXOs()
	for subnetID, utxos := range utxoSets {
		for _, utxo := range utxos {
			require.NoError(
				t, globalUTXOs.AddUTXO(context.Background(), subnetID, constants.PlatformChainID, utxo),
			)
		}
	}
	return &DeterministicChainUTXOs{
		ChainUTXOs: wallet.NewChainUTXOs(constants.PlatformChainID, globalUTXOs),
	}
}

type DeterministicChainUTXOs struct {
	wallet.ChainUTXOs
}

func (c *DeterministicChainUTXOs) UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*lux.UTXO, error) {
	utxos, err := c.ChainUTXOs.UTXOs(ctx, sourceChainID)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(utxos, func(a, b *lux.UTXO) int {
		return a.Compare(&b.UTXOID)
	})
	return utxos, nil
}

// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/vms/components/lux"
)

type ChainUTXOs interface {
	AddUTXO(ctx context.Context, destinationChainID ids.ID, utxo *lux.UTXO) error
	RemoveUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) error

	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*lux.UTXO, error)
	GetUTXO(ctx context.Context, sourceChainID, utxoID ids.ID) (*lux.UTXO, error)
}

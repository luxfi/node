// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/vms/components/lux"
)

type UTXOGetter interface {
	GetUTXO(utxoID ids.ID) (*lux.UTXO, error)
}

type UTXOAdder interface {
	AddUTXO(utxo *lux.UTXO)
}

type UTXODeleter interface {
	DeleteUTXO(utxoID ids.ID)
}

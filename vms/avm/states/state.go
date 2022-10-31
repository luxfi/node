// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package states

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxdefi/luxd/database"
	"github.com/luxdefi/luxd/database/prefixdb"
	"github.com/luxdefi/luxd/vms/avm/txs"
	"github.com/luxdefi/luxd/vms/components/lux"
)

var (
	utxoPrefix      = []byte("utxo")
	statusPrefix    = []byte("status")
	singletonPrefix = []byte("singleton")
	txPrefix        = []byte("tx")

	_ State = (*state)(nil)
)

// State persistently maintains a set of UTXOs, transaction, statuses, and
// singletons.
type State interface {
	lux.UTXOState
	lux.StatusState
	lux.SingletonState
	TxState
}

type state struct {
	lux.UTXOState
	lux.StatusState
	lux.SingletonState
	TxState
}

func New(db database.Database, parser txs.Parser, metrics prometheus.Registerer) (State, error) {
	utxoDB := prefixdb.New(utxoPrefix, db)
	statusDB := prefixdb.New(statusPrefix, db)
	singletonDB := prefixdb.New(singletonPrefix, db)
	txDB := prefixdb.New(txPrefix, db)

	utxoState, err := lux.NewMeteredUTXOState(utxoDB, parser.Codec(), metrics)
	if err != nil {
		return nil, err
	}

	statusState, err := lux.NewMeteredStatusState(statusDB, metrics)
	if err != nil {
		return nil, err
	}

	txState, err := NewTxState(txDB, parser, metrics)
	return &state{
		UTXOState:      utxoState,
		StatusState:    statusState,
		SingletonState: lux.NewSingletonState(singletonDB),
		TxState:        txState,
	}, err
}

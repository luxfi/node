<<<<<<< HEAD:vms/avm/txs/mempool/mempool.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/txs/mempool/mempool.go
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/luxfi/metric"

<<<<<<< HEAD:vms/avm/txs/mempool/mempool.go
	"github.com/luxfi/node/vms/avm/txs"
	"github.com/luxfi/node/vms/txs/mempool"
)

func New(
	namespace string,
	registerer prometheus.Registerer,
) (mempool.Mempool[*txs.Tx], error) {
	metrics, err := mempool.NewMetrics(namespace, registerer)
=======
	common "github.com/luxfi/consensus/core"
	"github.com/luxfi/node/vms/xvm/txs"

	txmempool "github.com/luxfi/node/vms/txs/mempool"
)

var _ Mempool = (*mempool)(nil)

// Mempool contains transactions that have not yet been put into a block.
type Mempool interface {
	txmempool.Mempool[*txs.Tx]

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built if there is at least one transaction in the mempool.
	RequestBuildBlock()
}

type mempool struct {
	txmempool.Mempool[*txs.Tx]

	toEngine chan<- common.MessageType
}

func New(
	namespace string,
	registerer metric.Registerer,
	toEngine chan<- common.MessageType,
) (Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/txs/mempool/mempool.go
	if err != nil {
		return nil, err
	}
	return mempool.New[*txs.Tx](metrics), nil
}

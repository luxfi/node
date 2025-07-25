// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/txs/mempool"
)

func New(
	namespace string,
	registerer prometheus.Registerer,
) (mempool.Mempool[*txs.Tx], error) {
	metrics, err := mempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	return mempool.New[*txs.Tx](metrics), nil
}

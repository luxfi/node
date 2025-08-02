// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/v2/vms/txs/mempool"
	"github.com/luxfi/node/v2/vms/xvm/txs"
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

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import "github.com/luxfi/metric"

type metrics struct {
	numTxsIndexed metric.Counter
}

func (m *metrics) initialize(namespace string, registerer metric.Registerer) error {
	m.numTxsIndexed = metric.NewCounter(metric.CounterOpts{
		Namespace: namespace,
		Name:      "txs_indexed",
		Help:      "Number of transactions indexed",
	})
	return registerer.Register(m.numTxsIndexed)
}

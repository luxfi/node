// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import metrics "github.com/luxfi/metric"

type metrics struct {
	numTxsIndexed metrics.Counter
}

func (m *metrics) initialize(namespace string, registerer metrics.Registerer) error {
	m.numTxsIndexed = metrics.NewCounter(metrics.CounterOpts{
		Namespace: namespace,
		Name:      "txs_indexed",
		Help:      "Number of transactions indexed",
	})
	return registerer.Register(m.numTxsIndexed)
}

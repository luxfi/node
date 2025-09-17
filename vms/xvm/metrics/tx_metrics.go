// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/luxfi/metric"

	"github.com/luxfi/node/vms/xvm/txs"
)

const txLabel = "tx"

var (
	_ txs.Visitor = (*txMetrics)(nil)

	txLabels = []string{txLabel}
)

type txMetrics struct {
	numTxs metrics.CounterVec
}

func newTxMetrics(registerer metrics.Registerer) (*txMetrics, error) {
	m := &txMetrics{
		numTxs: metrics.NewCounterVec(
			metrics.CounterOpts{
				Name: "txs_accepted",
				Help: "number of transactions accepted",
			},
			txLabels,
		),
	}
	return m, nil
}

func (m *txMetrics) BaseTx(*txs.BaseTx) error {
	m.numTxs.With(metrics.Labels{
		txLabel: "base",
	}).Inc()
	return nil
}

func (m *txMetrics) CreateAssetTx(*txs.CreateAssetTx) error {
	m.numTxs.With(metrics.Labels{
		txLabel: "create_asset",
	}).Inc()
	return nil
}

func (m *txMetrics) OperationTx(*txs.OperationTx) error {
	m.numTxs.With(metrics.Labels{
		txLabel: "operation",
	}).Inc()
	return nil
}

func (m *txMetrics) ImportTx(*txs.ImportTx) error {
	m.numTxs.With(metrics.Labels{
		txLabel: "import",
	}).Inc()
	return nil
}

func (m *txMetrics) ExportTx(*txs.ExportTx) error {
	m.numTxs.With(metrics.Labels{
		txLabel: "export",
	}).Inc()
	return nil
}

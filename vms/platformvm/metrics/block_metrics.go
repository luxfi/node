// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/luxfi/metric"

	"github.com/luxfi/node/vms/platformvm/block"
)

const blkLabel = "blk"

var (
	_ block.Visitor = (*blockMetrics)(nil)

	blkLabels = []string{blkLabel}
)

type blockMetrics struct {
	txMetrics *txMetrics
	numBlocks metric.CounterVec
}

func newBlockMetrics(registerer metric.Registerer) (*blockMetrics, error) {
	txMetrics, err := newTxMetrics(registerer)
	if err != nil {
		return nil, err
	}

	m := &blockMetrics{
		txMetrics: txMetrics,
		numBlocks: metric.NewCounterVec(
			metric.CounterOpts{
				Name: "blks_accepted",
				Help: "number of blocks accepted",
			},
			blkLabels,
		),
	}
	return m, nil
}

func (m *blockMetrics) AbortBlock(*block.AbortBlock) error {
	m.numBlocks.With(metric.Labels{
		blkLabel: "abort",
	}).Inc()
	return nil
}

func (m *blockMetrics) CommitBlock(*block.CommitBlock) error {
	m.numBlocks.With(metric.Labels{
		blkLabel: "commit",
	}).Inc()
	return nil
}

func (m *blockMetrics) ProposalBlock(b *block.ProposalBlock) error {
	m.numBlocks.With(metric.Labels{
		blkLabel: "proposal",
	}).Inc()
	for _, tx := range b.Transactions {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return b.Tx.Unsigned.Visit(m.txMetrics)
}

func (m *blockMetrics) StandardBlock(b *block.StandardBlock) error {
	m.numBlocks.With(metric.Labels{
		blkLabel: "standard",
	}).Inc()
	for _, tx := range b.Transactions {
		if err := tx.Unsigned.Visit(m.txMetrics); err != nil {
			return err
		}
	}
	return nil
}

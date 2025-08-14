// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"time"
	

	"github.com/prometheus/client_golang/prometheus"

	common "github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"

	txmempool "github.com/luxfi/node/vms/txs/mempool"
)

var (
	_ Mempool = (*mempool)(nil)

	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
	errMempoolFull                = errors.New("mempool is full")
)

type Mempool interface {
	txmempool.Mempool[*txs.Tx]

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built. If [emptyBlockPermitted] is true, the notification will be sent
	// regardless of whether there are no transactions in the mempool. If not,
	// a notification will only be sent if there is at least one transaction in
	// the mempool.
	RequestBuildBlock(emptyBlockPermitted bool)
	
	// Additional methods for compatibility
	HasTxs() bool
	Has(txID ids.ID) bool
	PeekTxs(n int) []*txs.Tx
	DropExpiredStakerTxs(minStartTime time.Time) []ids.ID
}

type mempool struct {
	txmempool.Mempool[*txs.Tx]

	toEngine chan<- common.Message
	bytesAvailable int  // Exposed for tests
	
	// Keep track of tx sizes for removal
	txSizes map[ids.ID]int
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
) (Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &mempool{
		Mempool:  pool,
		toEngine: toEngine,
		bytesAvailable: 64 * 1024 * 1024, // 64 MiB default
		txSizes: make(map[ids.ID]int),
	}, nil
}

func (m *mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
	}

	// Check if mempool has space
	txSize := len(tx.Bytes())
	if txSize > m.bytesAvailable {
		return errMempoolFull
	}

	err := m.Mempool.Add(tx)
	if err == nil {
		m.bytesAvailable -= txSize
		m.txSizes[tx.ID()] = txSize
	}
	return err
}

func (m *mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && m.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

func (m *mempool) HasTxs() bool {
	return m.Len() > 0
}

func (m *mempool) Has(txID ids.ID) bool {
	_, exists := m.Get(txID)
	return exists
}

func (m *mempool) PeekTxs(n int) []*txs.Tx {
	var result []*txs.Tx
	count := 0
	m.Iterate(func(tx *txs.Tx) bool {
		if count >= n {
			return false
		}
		result = append(result, tx)
		count++
		return true
	})
	return result
}

func (m *mempool) DropExpiredStakerTxs(minStartTime time.Time) []ids.ID {
	var droppedTxIDs []ids.ID
	var txsToRemove []*txs.Tx
	
	m.Iterate(func(tx *txs.Tx) bool {
		// Check if this is a staker transaction
		switch stakerTx := tx.Unsigned.(type) {
		case *txs.AddValidatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddDelegatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddPermissionlessValidatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddPermissionlessDelegatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		}
		return true
	})
	
	if len(txsToRemove) > 0 {
		m.Remove(txsToRemove...)
	}
	
	return droppedTxIDs
}

func (m *mempool) Remove(txs ...*txs.Tx) {
	for _, tx := range txs {
		if size, ok := m.txSizes[tx.ID()]; ok {
			m.bytesAvailable += size
			delete(m.txSizes, tx.ID())
		}
	}
	m.Mempool.Remove(txs...)
}

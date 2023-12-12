// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"time"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/utils/heap"
	"github.com/luxdefi/node/vms/platformvm/txs"
)

var _ TimedHeap = (*byEndTime)(nil)

type TimedHeap interface {
	Heap

	Timestamp() time.Time
}

type byEndTime struct {
	txHeap
}

func NewByEndTime() TimedHeap {
	return &byEndTime{
		txHeap: txHeap{
			heap: heap.NewMap[ids.ID, heapTx](func(a, b heapTx) bool {
				aTime := a.tx.Unsigned.(txs.Staker).EndTime()
				bTime := b.tx.Unsigned.(txs.Staker).EndTime()
				return aTime.Before(bTime)
			}),
		},
	}
}

func (h *byEndTime) Timestamp() time.Time {
	return h.Peek().Unsigned.(txs.Staker).EndTime()
}

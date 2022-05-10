// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

var _ Heap = &txHeapByAge{}

type txHeapByAge struct {
	txHeap
}

func NewTxHeapByAge() Heap {
	h := &txHeapByAge{}
	h.initialize(h)
	return h
}

func (h *txHeapByAge) Less(i, j int) bool {
	return h.txs[i].age < h.txs[j].age
}
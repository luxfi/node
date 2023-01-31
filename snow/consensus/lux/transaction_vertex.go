<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 8fb2bec88 (Must keep bloodline pure)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
=======
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
>>>>>>> 53a8245a8 (Update consensus)
)

var _ snowstorm.Tx = (*transactionVertex)(nil)

// newTransactionVertex returns a new transactionVertex initialized with a
// processing status.
func newTransactionVertex(vtx Vertex, nodes map[ids.ID]*transactionVertex) *transactionVertex {
	return &transactionVertex{
		vtx:    vtx,
		nodes:  nodes,
		status: choices.Processing,
	}
}

type transactionVertex struct {
	// vtx is the vertex that this transaction is attempting to confirm.
	vtx Vertex

	// nodes is used to look up other transaction vertices that are currently
	// processing. This is used to get parent vertices of this transaction.
	nodes map[ids.ID]*transactionVertex

	// status reports the status of this transaction vertex in snowstorm which
	// is then used by lux to determine the accaptability of the vertex.
	status choices.Status
}

<<<<<<< HEAD
func (tv *transactionVertex) Bytes() []byte {
=======
func (*transactionVertex) Bytes() []byte {
>>>>>>> 53a8245a8 (Update consensus)
	// Snowstorm uses the bytes of the transaction to broadcast through the
	// decision dispatcher. Because this is an internal transaction type, we
	// don't want to have this transaction broadcast. So, we return nil here.
	return nil
}

func (tv *transactionVertex) ID() ids.ID {
	return tv.vtx.ID()
}

<<<<<<< HEAD
func (tv *transactionVertex) Accept() error {
=======
func (tv *transactionVertex) Accept(context.Context) error {
>>>>>>> 53a8245a8 (Update consensus)
	tv.status = choices.Accepted
	return nil
}

<<<<<<< HEAD
func (tv *transactionVertex) Reject() error {
=======
func (tv *transactionVertex) Reject(context.Context) error {
>>>>>>> 53a8245a8 (Update consensus)
	tv.status = choices.Rejected
	return nil
}

<<<<<<< HEAD
func (tv *transactionVertex) Status() choices.Status { return tv.status }

// Verify isn't called in the consensus code. So this implementation doesn't
// really matter. However it's used to implement the tx interface.
func (tv *transactionVertex) Verify() error { return nil }
=======
func (tv *transactionVertex) Status() choices.Status {
	return tv.status
}

// Verify isn't called in the consensus code. So this implementation doesn't
// really matter. However it's used to implement the tx interface.
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (*transactionVertex) Verify(context.Context) error {
	return nil
}
=======
func (*transactionVertex) Verify() error { return nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
=======
func (*transactionVertex) Verify() error {
=======
func (*transactionVertex) Verify(context.Context) error {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
	return nil
}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
>>>>>>> 53a8245a8 (Update consensus)

// Dependencies returns the currently processing transaction vertices of this
// vertex's parents.
func (tv *transactionVertex) Dependencies() ([]snowstorm.Tx, error) {
	parents, err := tv.vtx.Parents()
	if err != nil {
		return nil, err
	}
	txs := make([]snowstorm.Tx, 0, len(parents))
	for _, parent := range parents {
		if parentTx, ok := tv.nodes[parent.ID()]; ok {
			txs = append(txs, parentTx)
		}
	}
	return txs, nil
}

// InputIDs must return a non-empty slice to avoid having the snowstorm engine
<<<<<<< HEAD
// vaciously accept it. A slice is returned containing just the vertexID in
// order to produce no conflicts based on the consumed input.
func (tv *transactionVertex) InputIDs() []ids.ID { return []ids.ID{tv.vtx.ID()} }

func (tv *transactionVertex) HasWhitelist() bool { return tv.vtx.HasWhitelist() }

func (tv *transactionVertex) Whitelist() (ids.Set, error) { return tv.vtx.Whitelist() }
=======
// vacuously accept it. A slice is returned containing just the vertexID in
// order to produce no conflicts based on the consumed input.
func (tv *transactionVertex) InputIDs() []ids.ID {
	return []ids.ID{tv.vtx.ID()}
}

func (tv *transactionVertex) HasWhitelist() bool {
	return tv.vtx.HasWhitelist()
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (tv *transactionVertex) Whitelist(ctx context.Context) (set.Set[ids.ID], error) {
	return tv.vtx.Whitelist(ctx)
=======
func (tv *transactionVertex) Whitelist() (ids.Set, error) {
	return tv.vtx.Whitelist()
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (tv *transactionVertex) Whitelist(ctx context.Context) (ids.Set, error) {
=======
func (tv *transactionVertex) Whitelist(ctx context.Context) (set.Set[ids.ID], error) {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
	return tv.vtx.Whitelist(ctx)
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
}
>>>>>>> 53a8245a8 (Update consensus)

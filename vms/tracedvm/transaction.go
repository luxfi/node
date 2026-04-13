// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracedvm

import (
	"context"

	"github.com/luxfi/consensus/engine/dag"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/trace"
)

var _ dag.Transaction = (*tracedTransaction)(nil)

type tracedTransaction struct {
	dag.Transaction

	tracer trace.Tracer
}

func (t *tracedTransaction) ID() ids.ID {
	return t.Transaction.ID()
}

func (t *tracedTransaction) Parent() ids.ID {
	return t.Transaction.Parent()
}

func (t *tracedTransaction) Height() uint64 {
	return t.Transaction.Height()
}

func (t *tracedTransaction) Bytes() []byte {
	return t.Transaction.Bytes()
}

func (t *tracedTransaction) Verify(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "tracedTransaction.Verify", trace.WithAttributes(
		trace.Stringer("txID", t.Transaction.ID()),
	))
	defer span.End()

	return t.Transaction.Verify(ctx)
}

func (t *tracedTransaction) Accept(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "tracedTransaction.Accept", trace.WithAttributes(
		trace.Stringer("txID", t.Transaction.ID()),
	))
	defer span.End()

	return t.Transaction.Accept(ctx)
}

func (t *tracedTransaction) Reject(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "tracedTransaction.Reject", trace.WithAttributes(
		trace.Stringer("txID", t.Transaction.ID()),
	))
	defer span.End()

	return t.Transaction.Reject(ctx)
}

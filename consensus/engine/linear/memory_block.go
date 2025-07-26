// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	"context"

	"github.com/luxfi/node/consensus/engine/linear/ancestor"
	"github.com/luxfi/node/consensus/linear"
)

var _ linear.Block = (*memoryBlock)(nil)

// memoryBlock wraps a linear Block to manage non-verified blocks
type memoryBlock struct {
	linear.Block

	tree    ancestor.Tree
	metrics *metrics
}

// Accept accepts the underlying block & removes sibling subtrees
func (mb *memoryBlock) Accept(ctx context.Context) error {
	mb.tree.RemoveDescendants(mb.Parent())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Accept(ctx)
}

// Reject rejects the underlying block & removes child subtrees
func (mb *memoryBlock) Reject(ctx context.Context) error {
	mb.tree.RemoveDescendants(mb.ID())
	mb.metrics.numNonVerifieds.Set(float64(mb.tree.Len()))
	return mb.Block.Reject(ctx)
}

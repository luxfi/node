// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

import (
	sampling "github.com/luxfi/node/consensus/sampling"
	"github.com/luxfi/node/ids"
)

// Tracks the state of a chain block
type chainBlock struct {
	t *Topological

	// block that this node contains. For the genesis, this value will be nil
	blk Block

	// shouldFalter is set to true if this node, and all its descendants received
	// less than Alpha votes
	shouldFalter bool

	// sb is the confidence instance used to decide which child is the canonical
	// child of this block. If this node has not had a child issued under it,
	// this value will be nil
	sb sampling.Consensus

	// children is the set of blocks that have been issued that name this block
	// as their parent. If this node has not had a child issued under it, this value
	// will be nil
	children map[ids.ID]Block
}

func (n *chainBlock) AddChild(child Block) {
	childID := child.ID()

	// if the confidence instance is nil, this is the first child. So the instance
	// should be initialized.
	if n.sb == nil {
		n.sb = sampling.NewTree(n.t.Factory, n.t.params, childID)
		n.children = make(map[ids.ID]Block)
	} else {
		n.sb.Add(childID)
	}

	n.children[childID] = child
}

func (n *chainBlock) Decided() bool {
	// if the block is nil, then this is the genesis which is defined as
	// accepted
	return n.blk == nil || n.blk.Height() <= n.t.lastAcceptedHeight
}

// Package block provides block chain engine functionality
package block

import (
	"time"

	"github.com/luxfi/ids"
)

// Context provides information about the block context for predicates
type Context struct {
	// PChainHeight is the P-Chain height when this block was accepted
	PChainHeight uint64
	// Timestamp is the timestamp of the block
	Timestamp time.Time
	// BlockID is the ID of the block being processed
	BlockID ids.ID
}

// Block represents a block in the blockchain
type Block interface {
	// ID returns the block's unique identifier
	ID() string

	// Height returns the block's height
	Height() uint64

	// Parent returns the parent block's ID
	Parent() string

	// Timestamp returns the block's timestamp
	Timestamp() int64
}

// Builder builds new blocks
type Builder interface {
	// Build creates a new block
	Build() (Block, error)
}

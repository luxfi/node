// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/txs"
)

// Block defines the common stateless interface for all blocks
type Block interface {
	consensus.ContextInitializable

	ID() ids.ID
	Parent() ids.ID
	Height() uint64
	// Timestamp that this block was created at
	Timestamp() time.Time
	MerkleRoot() ids.ID
	Bytes() []byte

	// Txs returns the transactions contained in the block
	Txs() []*txs.Tx

	// note: initialize does not assume that the transactions are initialized,
	// and initializes them itself.
	initialize(bytes []byte, cm codec.Manager) error
}

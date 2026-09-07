// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/txs"
)

// Block defines the common stateless interface for all blocks. There is no
// codec: a block is a self-describing native-ZAP buffer whose bytes are
// authoritative (ID = hash(bytes)); see Parser.ParseBlock.
type Block interface {
	ID() ids.ID
	Parent() ids.ID
	Height() uint64
	// Timestamp that this block was created at
	Timestamp() time.Time
	MerkleRoot() ids.ID
	Bytes() []byte

	// Txs returns the transactions contained in the block
	Txs() []*txs.Tx
}

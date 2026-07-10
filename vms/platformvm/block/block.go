// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/runtime"
)

// RuntimeInitializable binds a runtime into a block's embedded txs.
type RuntimeInitializable interface {
	InitRuntime(rt *runtime.Runtime)
}

// Block is the common stateless interface for all P-chain blocks. Every block
// is a single native-ZAP object; there is no codec and no initialize step —
// the bytes are authoritative and blocks are built by New*Block or read by
// Parse, both of which bind ID = hash(bytes).
type Block interface {
	RuntimeInitializable
	ID() ids.ID
	Parent() ids.ID
	Bytes() []byte
	Height() uint64

	// Txs returns the transactions contained in the block.
	Txs() []*txs.Tx

	// Visit calls [visitor] with this block's concrete type.
	Visit(visitor Visitor) error
}

// TimestampedBlock is a Block that carries a per-block timestamp. Every
// canonical P-chain block kind is timestamped.
type TimestampedBlock interface {
	Block
	Timestamp() time.Time
}

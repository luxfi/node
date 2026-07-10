// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// CodecVersion is the sole write version for P-Chain genesis blobs. It
// is shared with the block codec (and tx codec) so a single constant
// pins the whole stack.
const CodecVersion = block.CodecVersion

// Codec is the codec.Manager used to (un)marshal P-Chain genesis blobs.
// It aliases txs.GenesisCodec (unbounded size budget) because the outer
// Genesis struct has no slot ID — every version-sensitive value lives in
// the embedded []*txs.Tx (validators, chains), so decoding via the tx
// genesis codec is exactly the dispatch we need. One ZAP-native version,
// one write path, one read path.
var Codec = txs.GenesisCodec

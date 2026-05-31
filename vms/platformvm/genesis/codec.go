// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// CodecVersion is the canonical write version for new P-Chain genesis
// blobs. It is shared with the block codec (and txs codec) so a single
// constant bumps the whole stack.
const CodecVersion = block.CodecVersion

// Codec is the multi-version codec.Manager used to (un)marshal P-Chain
// genesis blobs. It is intentionally an alias for txs.GenesisCodec
// rather than block.GenesisCodec because:
//
//   - txs.GenesisCodec registers BOTH the v0 (v1.23.x Apricot/Banff)
//     and v1 (current) tx slot maps, so it can decode v0-prefixed
//     cached-genesis blobs that pre-date the codec bump. block.Genesis
//     Codec carries only the v1 slot map and errors on prefix=0 with
//     codec.ErrUnknownVersion — exactly the failure mode that broke
//     v1.28.0 testnet canary at first-byte parse of
//     <data>/genesis.bytes.
//   - The outer Genesis struct itself has no slot ID; all version-
//     sensitive data lives in the embedded []*txs.Tx (validators,
//     chains). Decoding via txs.GenesisCodec is therefore exactly the
//     dispatch we need — version is taken from the 2-byte wire prefix
//     and routed to the v0 or v1 tx slot map.
//   - Marshal at CodecVersion (== v1) is unchanged: the v1 slot map in
//     txs.GenesisCodec is byte-for-byte identical to the v1 slot map in
//     block.GenesisCodec (both come from registerV1TxTypes).
//
// All write paths (Genesis.Bytes, New, static_service) MUST continue to
// use CodecVersion. v0 is a READ-ONLY decoder.
var Codec = txs.GenesisCodec

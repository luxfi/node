// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"fmt"

	"github.com/luxfi/codec"
	"github.com/luxfi/node/vms/platformvm/block/v0"
)

// ErrShortBytes is returned when the input is shorter than the 2-byte
// codec-version prefix.
var ErrShortBytes = errors.New("block bytes too short for codec version prefix")

// Parse decodes a block byte stream produced by either the v0
// (v1.23.x) or v1 (current) block codec. The codec.Manager argument
// selects the SIZE class — Codec (1 MiB max) or GenesisCodec
// (unbounded). The actual VERSION is taken from the 2-byte wire prefix
// and dispatched to the matching slot map.
//
// Parse never re-marshals the input; BlockID = hash(b) verbatim and
// b is stashed on the returned block's CommonBlock. This is the
// byte-preserving path that keeps BlockIDs (and inner TxIDs) stable
// across the v0->v1 migration.
//
// c must be one of block.Codec or block.GenesisCodec. The v0 codec is
// selected internally based on the size class of c. Other codecs are
// rejected with codec.ErrUnknownVersion.
func Parse(c codec.Manager, b []byte) (Block, error) {
	if len(b) < 2 {
		return nil, ErrShortBytes
	}
	version := uint16(b[0])<<8 | uint16(b[1])

	switch version {
	case CodecVersionV1:
		return parseV1(c, b)
	case CodecVersionV0:
		return parseV0(c, b)
	default:
		return nil, fmt.Errorf("%w: %d", codec.ErrUnknownVersion, version)
	}
}

// parseV1 is the canonical block-decode path. Bytes are unmarshalled
// against the v1 slot map and the resulting Block keeps b as its
// authoritative bytes.
func parseV1(c codec.Manager, b []byte) (Block, error) {
	var blk Block
	if _, err := c.Unmarshal(b, &blk); err != nil {
		return nil, err
	}
	return blk, blk.initialize(b)
}

// parseV0 decodes a v1.23.x ("Apricot/Banff") block. The v0 slot map
// is closed; the result is a v0-typed block (one of v0.ApricotXxx /
// v0.BanffXxx) that is wrapped in an adapter implementing block.Block.
//
// The adapter:
//   - reports the original bytes verbatim (Bytes() returns b),
//   - computes BlockID = hash(b),
//   - translates Visit calls to the canonical v1 visitor
//     (ApricotProposalBlock/BanffProposalBlock -> Visitor.ProposalBlock,
//     ApricotStandardBlock/BanffStandardBlock -> Visitor.StandardBlock,
//     etc.),
//   - re-initializes embedded txs using their byte-preserving
//     InitializeFromBytes path against the v0 tx codec, so inner TxIDs
//     remain hash(originalTxBytes) under the v0 layout.
//
// ApricotAtomicBlock is decoded but rejected at the block boundary: the
// modern P-only network does not accept new atomic blocks, and any
// pre-Banff atomic block left on disk should have been replaced by the
// Banff history. We return an explicit error so the caller can decide
// whether to surface it as DB corruption or as a soft skip.
func parseV0(c codec.Manager, b []byte) (Block, error) {
	v0c := v0CodecFor(c)
	var v0blk v0.Block
	if _, err := v0c.Unmarshal(b, &v0blk); err != nil {
		return nil, err
	}
	return liftV0(v0blk, b)
}

// v0CodecFor returns the v0 codec.Manager that matches the size class
// of c. We mirror the size class so that a GenesisCodec caller (large
// max size) gets the v0GenesisCodec (also large) rather than v0Codec
// (1 MiB).
func v0CodecFor(c codec.Manager) codec.Manager {
	if c == GenesisCodec {
		return v0GenesisCodec
	}
	return v0Codec
}

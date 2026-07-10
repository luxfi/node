// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/luxfi/node/vms/pcodecs"
)

// ErrShortBytes is returned when the input is shorter than the 2-byte
// codec-version prefix.
var ErrShortBytes = errors.New("block bytes too short for codec version prefix")

// Parse decodes a block byte stream produced by the ZAP-native block
// codec. c selects the SIZE class — Codec (1 MiB max) or GenesisCodec
// (unbounded). The version is taken from the 2-byte wire prefix; a
// prefix other than CodecVersion is rejected by c.Unmarshal with
// ErrUnknownVersion.
//
// Parse never re-marshals the input: BlockID = hash(b) verbatim and b
// is stashed on the returned block's CommonBlock. This byte-preserving
// path keeps BlockIDs (and inner TxIDs) stable.
func Parse(c pcodecs.Manager, b []byte) (Block, error) {
	if len(b) < 2 {
		return nil, ErrShortBytes
	}
	var blk Block
	if _, err := c.Unmarshal(b, &blk); err != nil {
		return nil, err
	}
	return blk, blk.initialize(b)
}

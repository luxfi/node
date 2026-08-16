// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// Parse dispatch. There is no codec: Parse wraps the buffer zero-copy and
// dispatches on the 1-byte blockKind at object offset 0. Parse never
// re-marshals the input; ID = hash(b) verbatim and b is the block's
// authoritative bytes.

import (
	"fmt"

	"github.com/luxfi/zap"
)

// Parse decodes a native-ZAP P-chain block. It reads the blockKind
// discriminator and wraps the matching typed block over b (byte-preserving).
func Parse(b []byte) (Block, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse block: %w", err)
	}
	switch k := blockKind(msg.Root().Uint8(offBlkKind)); k {
	case blkAbort:
		blk := &AbortBlock{}
		return blk, blk.setID(b)
	case blkCommit:
		blk := &CommitBlock{}
		return blk, blk.setID(b)
	case blkProposal:
		blk := &ProposalBlock{}
		if err := blk.setID(b); err != nil {
			return nil, err
		}
		if blk.Tx() == nil {
			return nil, errNoProposalTx
		}
		return blk, nil
	case blkStandard:
		blk := &StandardBlock{}
		return blk, blk.setID(b)
	default:
		return nil, fmt.Errorf("zap: unknown block kind %d", k)
	}
}

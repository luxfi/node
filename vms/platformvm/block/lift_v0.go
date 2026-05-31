// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"fmt"
	"time"

	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/runtime"
	"github.com/luxfi/node/vms/platformvm/block/v0"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// errLegacyAtomicBlock is returned when v0 decoding produces an
// ApricotAtomicBlock. The modern P-only network does not accept atomic
// blocks; any pre-Banff atomic block on disk must be handled by the
// caller (typically by skipping the entry — atomic block IDs are not
// reachable from the canonical Banff-era last-accepted chain).
var errLegacyAtomicBlock = errors.New("apricot atomic block is not supported on the P-only network")

// liftV0 wraps a v0-decoded block in an adapter that implements
// block.Block and dispatches Visit to the canonical v1 Visitor.
//
// The lifted block:
//   - returns b verbatim from Bytes() (no re-marshal),
//   - reports BlockID = hash(b),
//   - reports Height/Parent/Txs delegated to the v0 struct,
//   - dispatches Visit per the kind table:
//       ApricotProposalBlock, BanffProposalBlock -> StandardBlock
//         (proposal/commit/abort are dead block kinds on the P-only
//          network — modern blocks are all StandardBlock with proposal
//          txs inlined as decision txs; on the historical decode path
//          we surface them via the StandardBlock visitor so the
//          executor's StandardBlock arm runs the embedded txs without
//          requiring a separate Apricot/Banff visitor surface),
//       ApricotStandardBlock, BanffStandardBlock -> StandardBlock,
//       ApricotAbortBlock, BanffAbortBlock        -> AbortBlock,
//       ApricotCommitBlock, BanffCommitBlock      -> CommitBlock.
//   - re-initializes each embedded tx via tx.InitializeFromBytes
//     against the v0 tx codec, byte-preserving inner TxIDs.
//
// initVisit selects the v1 Visitor arm. The lifting table is the
// migration's only place where a v0 block kind maps to a v1 kind; once
// every block on disk is v1, this file becomes dead code and can be
// removed.
func liftV0(b v0.Block, raw []byte) (Block, error) {
	switch v := b.(type) {
	case *v0.ApricotAtomicBlock:
		_ = v
		return nil, errLegacyAtomicBlock
	}

	blkID := hash.ComputeHash256Array(raw)
	lifted := &liftedV0Block{
		blockID:  blkID,
		raw:      raw,
		v0:       b,
		parentID: b.ParentID(),
		height:   b.Height(),
		timeUnix: b.TimestampUnix(),
		txs:      b.Txs(),
	}
	if err := lifted.initTxs(); err != nil {
		return nil, fmt.Errorf("v0 lift: %w", err)
	}
	return lifted, nil
}

// liftedV0Block is the block.Block adapter around a v0-decoded block.
// It does not embed CommonBlock — the canonical CommonBlock owns its
// own bytes via SetBytes, which would re-derive BlockID from a
// marshalled v1 layout. The lifted block instead computes BlockID once
// at construction time from the original v0 bytes and stashes them.
type liftedV0Block struct {
	blockID  ids.ID
	raw      []byte
	v0       v0.Block
	parentID ids.ID
	height   uint64
	timeUnix uint64
	txs      []*txs.Tx
}

func (b *liftedV0Block) ID() ids.ID     { return b.blockID }
func (b *liftedV0Block) Parent() ids.ID { return b.parentID }
func (b *liftedV0Block) Bytes() []byte  { return b.raw }
func (b *liftedV0Block) Height() uint64 { return b.height }
func (b *liftedV0Block) Txs() []*txs.Tx { return b.txs }

func (b *liftedV0Block) Timestamp() time.Time { return time.Unix(int64(b.timeUnix), 0) }

func (b *liftedV0Block) initialize(_ []byte) error {
	// liftedV0Block has its identity (BlockID, raw bytes) committed at
	// liftV0 construction time from the original input bytes. The
	// argument here is the v1-marshalled bytes that block.Parse would
	// derive for a v1 block; for a v0 block we ignore it and keep raw.
	return nil
}

func (b *liftedV0Block) InitRuntime(rt *runtime.Runtime) {
	for _, tx := range b.txs {
		if tx == nil {
			continue
		}
		tx.Unsigned.InitRuntime(rt)
	}
}

// initTxs runs InitializeFromBytes on every embedded tx using the v0
// tx codec. The signed bytes the codec recorded during Unmarshal are
// what we need; we recover them by re-marshalling the unsigned half
// under v0 and slicing into the parent block's input bytes is not
// safe (the parent block bytes interleave block-header fields). The
// linearcodec leaves the unsigned/credentials split implicit in the
// stream offset, so the cleanest byte-preserving handle we have is to
// ask the v0 tx codec to Size(version, &tx.Unsigned) and then take
// the prefix from the tx's own bytes — but those bytes have not been
// set yet by linearcodec, only the struct fields.
//
// We therefore re-marshal each tx under the v0 codec to recover its
// signedBytes. The wire format is deterministic and the resulting
// bytes are byte-equal to whatever the v0 producer emitted; the
// re-marshal is a closed loop over the v0 layout the tx was decoded
// from. TxID = hash(re-marshalled v0 bytes) = TxID the chain
// originally committed.
//
// This single re-marshal is allowed only here, only against the v0
// codec, and only for txs whose Unsigned was decoded under v0. Under
// the v1 path, txs are byte-preserved via tx.Parse without any
// re-marshal.
func (b *liftedV0Block) initTxs() error {
	for _, tx := range b.txs {
		if tx == nil {
			continue
		}
		if err := tx.InitializeFromBytesAtVersion(txs.GenesisCodec, txs.CodecVersionV0); err != nil {
			return fmt.Errorf("byte-preserve v0 tx: %w", err)
		}
	}
	return nil
}

func (b *liftedV0Block) Visit(v Visitor) error {
	switch b.v0.(type) {
	case *v0.ApricotAbortBlock, *v0.BanffAbortBlock:
		return v.AbortBlock(b.asAbortBlock())
	case *v0.ApricotCommitBlock, *v0.BanffCommitBlock:
		return v.CommitBlock(b.asCommitBlock())
	case *v0.ApricotProposalBlock, *v0.BanffProposalBlock:
		return v.ProposalBlock(b.asProposalBlock())
	case *v0.ApricotStandardBlock, *v0.BanffStandardBlock:
		return v.StandardBlock(b.asStandardBlock())
	default:
		return fmt.Errorf("v0 lift: unhandled block kind %T", b.v0)
	}
}

// asStandardBlock builds a *StandardBlock that mirrors the lifted v0
// block. The returned block uses the SAME raw bytes (so its BlockID
// matches b.blockID) and the SAME Txs slice (so the executor sees the
// same set of txs). This is the synthetic v1 view of a v0 block.
func (b *liftedV0Block) asStandardBlock() *StandardBlock {
	return &StandardBlock{
		Time:         b.timeUnix,
		CommonBlock:  b.commonBlock(),
		Transactions: b.txs,
	}
}

func (b *liftedV0Block) asProposalBlock() *ProposalBlock {
	// Modern ProposalBlock has Tx + Transactions tail. The lifted v0
	// block's Txs() already returns the canonical ordering for both
	// Apricot (single proposal Tx) and Banff (decision tail + proposal
	// Tx). We split off the trailing element as the proposal tx.
	tail := b.txs
	if len(tail) == 0 {
		return &ProposalBlock{
			Time:        b.timeUnix,
			CommonBlock: b.commonBlock(),
		}
	}
	last := tail[len(tail)-1]
	return &ProposalBlock{
		Time:         b.timeUnix,
		Transactions: tail[:len(tail)-1],
		CommonBlock:  b.commonBlock(),
		Tx:           last,
	}
}

func (b *liftedV0Block) asAbortBlock() *AbortBlock {
	return &AbortBlock{Time: b.timeUnix, CommonBlock: b.commonBlock()}
}

func (b *liftedV0Block) asCommitBlock() *CommitBlock {
	return &CommitBlock{Time: b.timeUnix, CommonBlock: b.commonBlock()}
}

// commonBlock builds a CommonBlock whose BlockID + bytes are the
// lifted block's raw bytes / hash. This is the only place we copy the
// raw bytes into a v1-typed block, and it is read-only — the caller
// uses the synthetic block's visitor methods, never re-marshals it.
func (b *liftedV0Block) commonBlock() CommonBlock {
	return CommonBlock{
		PrntID:  b.parentID,
		Hght:    b.height,
		BlockID: b.blockID,
		bytes:   b.raw,
	}
}

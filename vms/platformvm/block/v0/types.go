// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package v0

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// Block is the v0-decoder-side counterpart of block.Block. It is
// satisfied by every concrete v0 block kind so that the v0 codec can
// dispatch on an interface destination at unmarshal time. The parent
// block package wraps the returned v0.Block in an adapter that
// implements block.Block (which in turn dispatches to the canonical v1
// Visitor).
type Block interface {
	// ParentID is the BlockID of the parent block.
	ParentID() ids.ID

	// Height is the block height. Genesis is at height 0.
	Height() uint64

	// TimestampUnix returns the block-header timestamp encoded in this
	// block (Banff and later). Apricot blocks carry no header timestamp
	// and return 0; the parent timestamp is used in their stead by the
	// adapter.
	TimestampUnix() uint64

	// Txs returns the txs carried by this block in their canonical
	// order. For ApricotProposalBlock the proposal Tx is the sole
	// element; for BanffProposalBlock the proposal Tx is the trailing
	// element (matching the v1.23.31 ProposalBlock.Txs() shape).
	Txs() []*txs.Tx
}

// CommonBlock holds the parent-id + height fields shared across every
// v0 block kind. Wire layout: PrntID (ids.ID, 32 bytes) followed by
// Hght (uint64, 8 bytes). Byte-equal to v1.23.31 CommonBlock.
type CommonBlock struct {
	PrntID ids.ID `serialize:"true" json:"parentID"`
	Hght   uint64 `serialize:"true" json:"height"`
}

// ApricotProposalBlock decodes slot 0. Carries no header timestamp;
// pre-Banff time advances were carried in AdvanceTimeTx, not in the
// block header.
type ApricotProposalBlock struct {
	CommonBlock `serialize:"true"`
	Tx          *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ApricotProposalBlock) ParentID() ids.ID { return b.PrntID }
func (b *ApricotProposalBlock) Height() uint64   { return b.Hght }
func (*ApricotProposalBlock) TimestampUnix() uint64 { return 0 }
func (b *ApricotProposalBlock) Txs() []*txs.Tx   { return []*txs.Tx{b.Tx} }

// ApricotAbortBlock decodes slot 1.
type ApricotAbortBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotAbortBlock) ParentID() ids.ID { return b.PrntID }
func (b *ApricotAbortBlock) Height() uint64   { return b.Hght }
func (*ApricotAbortBlock) TimestampUnix() uint64 { return 0 }
func (*ApricotAbortBlock) Txs() []*txs.Tx     { return nil }

// ApricotCommitBlock decodes slot 2.
type ApricotCommitBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotCommitBlock) ParentID() ids.ID { return b.PrntID }
func (b *ApricotCommitBlock) Height() uint64   { return b.Hght }
func (*ApricotCommitBlock) TimestampUnix() uint64 { return 0 }
func (*ApricotCommitBlock) Txs() []*txs.Tx     { return nil }

// ApricotStandardBlock decodes slot 3.
type ApricotStandardBlock struct {
	CommonBlock  `serialize:"true"`
	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *ApricotStandardBlock) ParentID() ids.ID { return b.PrntID }
func (b *ApricotStandardBlock) Height() uint64   { return b.Hght }
func (*ApricotStandardBlock) TimestampUnix() uint64 { return 0 }
func (b *ApricotStandardBlock) Txs() []*txs.Tx   { return b.Transactions }

// ApricotAtomicBlock decodes slot 4. The atomic block flow is dead on
// the modern P-only network — Apricot atomic blocks only ever appear
// pre-Banff in legacy chain history.
type ApricotAtomicBlock struct {
	CommonBlock `serialize:"true"`
	Tx          *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ApricotAtomicBlock) ParentID() ids.ID { return b.PrntID }
func (b *ApricotAtomicBlock) Height() uint64   { return b.Hght }
func (*ApricotAtomicBlock) TimestampUnix() uint64 { return 0 }
func (b *ApricotAtomicBlock) Txs() []*txs.Tx   { return []*txs.Tx{b.Tx} }

// BanffProposalBlock decodes slot 29. Carries a Banff-era per-block
// timestamp ahead of the embedded Apricot proposal block (which still
// holds the proposal Tx). The on-wire field order is Time, then
// Transactions (the decision-tx tail), then ApricotProposalBlock.
type BanffProposalBlock struct {
	Time                 uint64    `serialize:"true" json:"time"`
	Transactions         []*txs.Tx `serialize:"true" json:"txs"`
	ApricotProposalBlock `serialize:"true"`
}

func (b *BanffProposalBlock) TimestampUnix() uint64 { return b.Time }
func (b *BanffProposalBlock) Txs() []*txs.Tx {
	out := make([]*txs.Tx, len(b.Transactions)+1)
	copy(out, b.Transactions)
	out[len(b.Transactions)] = b.ApricotProposalBlock.Tx
	return out
}

// BanffAbortBlock decodes slot 30.
type BanffAbortBlock struct {
	Time              uint64 `serialize:"true" json:"time"`
	ApricotAbortBlock `serialize:"true"`
}

func (b *BanffAbortBlock) TimestampUnix() uint64 { return b.Time }

// BanffCommitBlock decodes slot 31.
type BanffCommitBlock struct {
	Time               uint64 `serialize:"true" json:"time"`
	ApricotCommitBlock `serialize:"true"`
}

func (b *BanffCommitBlock) TimestampUnix() uint64 { return b.Time }

// BanffStandardBlock decodes slot 32.
type BanffStandardBlock struct {
	Time                 uint64 `serialize:"true" json:"time"`
	ApricotStandardBlock `serialize:"true"`
}

func (b *BanffStandardBlock) TimestampUnix() uint64 { return b.Time }

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*RewardValidatorTx)(nil)

// RewardValidatorTx is a transaction that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked LUX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked LUX but no reward.
//
// The struct IS the wire: it holds the zap buffer and reads its field by
// offset. No codec, no marshal.
//
// Wire: zap header + object{ kind:u8@0, TxID:32B@1 }.
type RewardValidatorTx struct {
	msg *zap.Message
}

// offRewardTxID is the wire position of the removed staker's tx id (after
// kind@0).
const offRewardTxID = 1 // 32B, after kind@0

// NewRewardValidatorTx builds the tx into a fresh zap buffer — the one place a
// field becomes bytes (construction, not serialization).
func NewRewardValidatorTx(txID ids.ID) *RewardValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + 33)
	ob := b.StartObject(33)
	ob.SetUint8(offKind, uint8(kindRewardValidator))
	setID(ob, offRewardTxID, txID)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &RewardValidatorTx{msg: msg}
}

// TxID is the id of the tx that created the delegator/validator being
// removed/rewarded (offset read).
func (tx *RewardValidatorTx) TxID() ids.ID { return readID(tx.msg.Root(), offRewardTxID) }

// Bytes returns the underlying zap buffer literally — no encode.
func (tx *RewardValidatorTx) Bytes() []byte { return tx.msg.Bytes() }

// SetBytes wraps b as this tx's buffer (zero copy); the Parse dispatch uses it.
func (tx *RewardValidatorTx) SetBytes(b []byte) { tx.msg, _ = zap.Parse(b) }

func (*RewardValidatorTx) InitRuntime(*runtime.Runtime)           {}
func (*RewardValidatorTx) InputIDs() set.Set[ids.ID]              { return nil }
func (*RewardValidatorTx) Outputs() []*lux.TransferableOutput     { return nil }
func (*RewardValidatorTx) SyntacticVerify(*runtime.Runtime) error { return nil }
func (tx *RewardValidatorTx) Visit(visitor Visitor) error         { return visitor.RewardValidatorTx(tx) }
func (tx *RewardValidatorTx) Initialize(context.Context) error    { return nil }

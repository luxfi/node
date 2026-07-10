// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*AdvanceTimeTx)(nil)

// AdvanceTimeTx increases the chain's timestamp. The struct IS the wire: it
// holds the zap buffer and reads its field by offset. No codec, no marshal.
//
// Wire: zap header + object{ kind:u8@0, Time:u64@1 }.
type AdvanceTimeTx struct {
	msg *zap.Message
}

const offAdvanceTimeTime = 1 // u64, after kind@0

// NewAdvanceTimeTx builds the tx into a fresh zap buffer — the one place a
// field becomes bytes (construction, not serialization).
func NewAdvanceTimeTx(t uint64) *AdvanceTimeTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + 9)
	ob := b.StartObject(9)
	ob.SetUint8(offKind, uint8(kindAdvanceTime))
	ob.SetUint64(offAdvanceTimeTime, t)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &AdvanceTimeTx{msg: msg}
}

// Time is the unix time this tx proposes advancing the chain to (offset read).
func (tx *AdvanceTimeTx) Time() uint64 { return tx.msg.Root().Uint64(offAdvanceTimeTime) }

// Timestamp returns Time as a time.Time.
func (tx *AdvanceTimeTx) Timestamp() time.Time { return time.Unix(int64(tx.Time()), 0) }

// Bytes returns the underlying zap buffer literally — no encode.
func (tx *AdvanceTimeTx) Bytes() []byte { return tx.msg.Bytes() }

// SetBytes wraps b as this tx's buffer (zero copy); the Parse dispatch uses it.
func (tx *AdvanceTimeTx) SetBytes(b []byte) { tx.msg, _ = zap.Parse(b) }

func (*AdvanceTimeTx) InitRuntime(*runtime.Runtime)           {}
func (*AdvanceTimeTx) InputIDs() set.Set[ids.ID]              { return nil }
func (*AdvanceTimeTx) Outputs() []*lux.TransferableOutput     { return nil }
func (*AdvanceTimeTx) SyntacticVerify(*runtime.Runtime) error { return nil }
func (tx *AdvanceTimeTx) Visit(visitor Visitor) error         { return visitor.AdvanceTimeTx(tx) }
func (tx *AdvanceTimeTx) Initialize(context.Context) error    { return nil }

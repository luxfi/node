// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*CreateNetworkTx)(nil)

// CreateNetworkTx is an unsigned proposal to create a new network. The struct
// IS the wire: it embeds spendingTx (the envelope) and adds the owner header
// inline (threshold + locktime + addr-list ptr).
//
// Wire: zap header + object{ envelope@0..76, Owner{ threshold:u32@77,
// locktime:u64@81, addrs:listptr@89 } } (kind=kindCreateNetwork).
type CreateNetworkTx struct {
	spendingTx
}

const (
	offCreateNetOwnerThreshold = spendSize // u32
	offCreateNetOwnerLocktime  = 81        // u64
	offCreateNetOwnerAddrs     = 89        // list ptr (8B)
	sizeCreateNet              = 97
)

// NewCreateNetworkTx builds the tx into a fresh zap buffer.
func NewCreateNetworkTx(base *lux.BaseTx, owner fx.Owner) (*CreateNetworkTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + sizeCreateNet)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	threshold, locktime, addrOff, addrCount, err := writeOwner(b, owner)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(sizeCreateNet)
	setEnvelope(ob, kindCreateNetwork, base, p)
	setOwner(ob, offCreateNetOwnerThreshold, offCreateNetOwnerLocktime, offCreateNetOwnerAddrs, threshold, locktime, addrOff, addrCount)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &CreateNetworkTx{spendingTx{msg}}, nil
}

// Owner is who is authorized to manage this network (offset read).
func (tx *CreateNetworkTx) Owner() fx.Owner {
	return readOwner(tx.root(), offCreateNetOwnerThreshold, offCreateNetOwnerLocktime, offCreateNetOwnerAddrs)
}

// SyntacticVerify verifies that this transaction is well-formed.
func (tx *CreateNetworkTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return tx.Owner().Verify()
}

func (tx *CreateNetworkTx) Visit(visitor Visitor) error {
	return visitor.CreateNetworkTx(tx)
}

func (tx *CreateNetworkTx) Initialize(context.Context) error { return nil }

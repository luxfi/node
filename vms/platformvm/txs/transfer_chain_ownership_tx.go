// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*TransferChainOwnershipTx)(nil)

	ErrTransferPermissionlessChain = errors.New("cannot transfer ownership of a permissionless chain")
)

// TransferChainOwnershipTx re-owns a chain. The struct IS the wire: it embeds
// the spending envelope and reads its delta fields by offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	Chain           32B @ 77   (id)
//	ChainAuth       8B  @ 109  (auth ptr: sig-index list)
//	Owner.Threshold u32 @ 117
//	Owner.Locktime  u64 @ 121
//	Owner.Addrs     8B  @ 129  (addr list ptr)
type TransferChainOwnershipTx struct {
	spendingTx
}

const (
	offTransferChain           = spendSize // 77
	offTransferChainAuth       = 109
	offTransferOwnerThreshold  = 117
	offTransferOwnerLocktime   = 121
	offTransferOwnerAddrs      = 129
	sizeTransferChainOwnership = 137
)

// NewTransferChainOwnershipTx builds the tx into a fresh zap buffer.
func NewTransferChainOwnershipTx(base *lux.BaseTx, chain ids.ID, chainAuth verify.Verifiable, owner fx.Owner) (*TransferChainOwnershipTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeTransferChainOwnership)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}
	threshold, locktime, addrOff, addrCount, err := writeOwner(b, owner)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeTransferChainOwnership)
	setEnvelope(ob, kindTransferChainOwnership, base, p)
	setID(ob, offTransferChain, chain)
	ob.SetList(offTransferChainAuth, authOff, authCount)
	setOwner(ob, offTransferOwnerThreshold, offTransferOwnerLocktime, offTransferOwnerAddrs, threshold, locktime, addrOff, addrCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &TransferChainOwnershipTx{spendingTx{msg}}, nil
}

// Chain is the ID of the chain this tx is modifying (offset read).
func (tx *TransferChainOwnershipTx) Chain() ids.ID { return readID(tx.root(), offTransferChain) }

// ChainAuth proves the issuer has the right to modify the chain.
func (tx *TransferChainOwnershipTx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offTransferChainAuth)
}

// Owner is who is now authorized to manage this chain.
func (tx *TransferChainOwnershipTx) Owner() fx.Owner {
	return readOwner(tx.root(), offTransferOwnerThreshold, offTransferOwnerLocktime, offTransferOwnerAddrs)
}

func (tx *TransferChainOwnershipTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Chain() == constants.PrimaryNetworkID:
		return ErrTransferPermissionlessChain
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return verify.All(tx.ChainAuth(), tx.Owner())
}

func (tx *TransferChainOwnershipTx) Visit(visitor Visitor) error {
	return visitor.TransferChainOwnershipTx(tx)
}

// Initialize is a no-op; the struct is already the wire.
func (tx *TransferChainOwnershipTx) Initialize(ctx context.Context) error { return nil }

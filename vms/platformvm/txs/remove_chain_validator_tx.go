// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*RemoveChainValidatorTx)(nil)

	ErrRemovePrimaryNetworkValidator = errors.New("can't remove primary network validator with RemoveChainValidatorTx")
)

// RemoveChainValidatorTx removes a validator from a chain. The struct IS the
// wire: it embeds spendingTx (the envelope) and adds the node id, chain id,
// and an auth sig-index list.
//
// Wire: zap header + object{ envelope@0..76, NodeID:20B@77, Chain:32B@97,
// ChainAuth:listptr@129 } (kind=kindRemoveChainValidator).
type RemoveChainValidatorTx struct {
	spendingTx
}

const (
	offRemoveNodeID    = spendSize // 20B (ids.NodeIDLen)
	offRemoveChain     = 97        // 32B
	offRemoveChainAuth = 129       // list ptr (8B)
	sizeRemove         = 137
)

// NewRemoveChainValidatorTx builds the tx into a fresh zap buffer.
func NewRemoveChainValidatorTx(base *lux.BaseTx, nodeID ids.NodeID, chain ids.ID, chainAuth verify.Verifiable) (*RemoveChainValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + sizeRemove)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(sizeRemove)
	setEnvelope(ob, kindRemoveChainValidator, base, p)
	setNodeID(ob, offRemoveNodeID, nodeID)
	setID(ob, offRemoveChain, chain)
	ob.SetList(offRemoveChainAuth, authOff, authCount)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &RemoveChainValidatorTx{spendingTx{msg}}, nil
}

// NodeID is the node to remove from the chain (offset read).
func (tx *RemoveChainValidatorTx) NodeID() ids.NodeID {
	return readNodeID(tx.root(), offRemoveNodeID)
}

// Chain is the chain to remove the node from (offset read).
func (tx *RemoveChainValidatorTx) Chain() ids.ID {
	return readID(tx.root(), offRemoveChain)
}

// ChainAuth proves the issuer's right to remove the node from the chain
// (offset read).
func (tx *RemoveChainValidatorTx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offRemoveChainAuth)
}

func (tx *RemoveChainValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Chain() == constants.PrimaryNetworkID:
		return ErrRemovePrimaryNetworkValidator
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return tx.ChainAuth().Verify()
}

func (tx *RemoveChainValidatorTx) Visit(visitor Visitor) error {
	return visitor.RemoveChainValidatorTx(tx)
}

func (tx *RemoveChainValidatorTx) Initialize(context.Context) error { return nil }

// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/engine/core"
	consensuschain "github.com/luxfi/node/v2/quasar/chain"
	"github.com/luxfi/node/v2/message"
	"github.com/luxfi/node/v2/utils/linked"
	"github.com/luxfi/node/v2/utils/lock"
	"github.com/luxfi/node/v2/vms/example/xsvm/chain"
	"github.com/luxfi/node/v2/vms/example/xsvm/execute"
	"github.com/luxfi/node/v2/vms/example/xsvm/tx"

	smblock "github.com/luxfi/node/v2/quasar/engine/chain/block"
	xsblock "github.com/luxfi/node/v2/vms/example/xsvm/block"
)

const MaxTxsPerBlock = 10

var _ Builder = (*builder)(nil)

type Builder interface {
	SetPreference(preferred ids.ID)
	AddTx(ctx context.Context, tx *tx.Tx) error
	WaitForEvent(ctx context.Context) (core.Message, error)
	BuildBlock(ctx context.Context, blockContext *smblock.Context) (consensuschain.Block, error)
}

type builder struct {
	chainContext *quasar.Context
	chain        chain.Chain

	preference ids.ID
	// pendingTxsCond is awoken once there is at least one pending transaction.
	pendingTxsCond *lock.Cond
	pendingTxs     *linked.Hashmap[ids.ID, *tx.Tx]
}

func New(chainContext *quasar.Context, chain chain.Chain) Builder {
	return &builder{
		chainContext:   chainContext,
		chain:          chain,
		preference:     chain.LastAccepted(),
		pendingTxsCond: lock.NewCond(&sync.Mutex{}),
		pendingTxs:     linked.NewHashmap[ids.ID, *tx.Tx](),
	}
}

func (b *builder) SetPreference(preferred ids.ID) {
	b.preference = preferred
}

func (b *builder) AddTx(_ context.Context, newTx *tx.Tx) error {
	// TODO: verify [tx] against the currently preferred state
	txID, err := newTx.ID()
	if err != nil {
		return err
	}

	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	b.pendingTxs.Put(txID, newTx)
	b.pendingTxsCond.Broadcast()
	return nil
}

func (b *builder) WaitForEvent(ctx context.Context) (core.Message, error) {
	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	for b.pendingTxs.Len() == 0 {
		if err := b.pendingTxsCond.Wait(ctx); err != nil {
			return core.Message{}, err
		}
	}

	return core.Message{
		Type: message.NotifyOp,
		Body: &core.PendingTxs{},
	}, nil
}

func (b *builder) BuildBlock(ctx context.Context, blockContext *smblock.Context) (consensuschain.Block, error) {
	preferredBlk, err := b.chain.GetBlock(b.preference)
	if err != nil {
		return nil, err
	}

	preferredState, err := preferredBlk.State()
	if err != nil {
		return nil, err
	}

	parentTimestamp := time.Unix(int64(preferredBlk.Time()), 0)
	timestamp := time.Now().Truncate(time.Second)
	if timestamp.Before(parentTimestamp) {
		timestamp = parentTimestamp
	}

	wipBlock := xsblock.Stateless{
		ParentID:  b.preference,
		Timestamp: timestamp.Unix(),
		Height:    preferredBlk.Height() + 1,
	}

	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	currentState := versiondb.New(preferredState)
	for len(wipBlock.Txs) < MaxTxsPerBlock {
		txID, currentTx, exists := b.pendingTxs.Oldest()
		if !exists {
			break
		}
		b.pendingTxs.Delete(txID)

		sender, err := currentTx.SenderID()
		if err != nil {
			// This tx was invalid, drop it and continue block building
			continue
		}

		txState := versiondb.New(currentState)
		txExecutor := execute.Tx{
			Context:      ctx,
			ChainContext: b.chainContext,
			Database:     txState,
			BlockContext: blockContext,
			TxID:         txID,
			Sender:       sender,
			// TODO: populate fees
		}
		if err := currentTx.Unsigned.Visit(&txExecutor); err != nil {
			// This tx was invalid, drop it and continue block building
			continue
		}
		// versiondb tracks changes - we update currentState to be this new state
		currentState = txState

		wipBlock.Txs = append(wipBlock.Txs, currentTx)
	}
	return b.chain.NewBlock(&wipBlock)
}

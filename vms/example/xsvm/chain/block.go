// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"time"

	db "github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	consensuschain "github.com/luxfi/node/quasar/chain"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/example/xsvm/execute"

	smblock "github.com/luxfi/node/quasar/engine/chain/block"
	xsblock "github.com/luxfi/node/vms/example/xsvm/block"
)

const maxClockSkew = 10 * time.Second

var (
	_ Block = (*block)(nil)

	errMissingParent         = errors.New("missing parent block")
	errMissingChild          = errors.New("missing child block")
	errParentNotVerified     = errors.New("parent block has not been verified")
	errFutureTimestamp       = errors.New("future timestamp")
	errTimestampBeforeParent = errors.New("timestamp before parent")
	errWrongHeight           = errors.New("wrong height")
)

type Block interface {
	consensuschain.Block
	smblock.WithVerifyContext

	// State intends to return the new chain state following this block's
	// acceptance. The new chain state is built (but not persisted) following a
	// block's verification to allow block's descendants verification before
	// being accepted.
	State() (db.Database, error)
}

type block struct {
	*xsblock.Stateless

	chain *chain

	id    ids.ID
	bytes []byte

	state               *versiondb.Database
	verifiedChildrenIDs set.Set[ids.ID]
}

func (b *block) Parent() ids.ID {
	return b.ParentID
}

func (b *block) ID() string {
	return b.id.String()
}

func (b *block) Status() choices.Status {
	// TODO: Implement proper status tracking
	return choices.Processing
}

func (b *block) Bytes() []byte {
	return b.bytes
}

func (b *block) Height() uint64 {
	return b.Stateless.Height
}

func (b *block) Timestamp() time.Time {
	return b.Stateless.Time()
}

func (b *block) Time() uint64 {
	return uint64(b.Stateless.Time().Unix())
}

func (b *block) Verify(ctx context.Context) error {
	return b.VerifyWithContext(ctx, nil)
}

func (b *block) Accept() error {
	// versiondb commits immediately, no need to call Commit()

	// Following this block's acceptance, make sure that it's direct children
	// point to the base state, which now also contains this block's changes.
	for childID := range b.verifiedChildrenIDs {
		child, exists := b.chain.verifiedBlocks[childID]
		if !exists {
			return errMissingChild
		}
		if err := child.state.SetDatabase(b.chain.acceptedState); err != nil {
			return err
		}
	}

	b.chain.lastAcceptedID = b.id
	delete(b.chain.verifiedBlocks, b.ParentID)
	b.state = nil
	return nil
}

func (b *block) Reject() error {
	delete(b.chain.verifiedBlocks, b.id)
	b.state = nil

	// TODO: push transactions back into the mempool
	return nil
}

func (b *block) ShouldVerifyWithContext(context.Context) (bool, error) {
	return execute.ExpectsContext(b.Stateless)
}

func (b *block) VerifyWithContext(ctx context.Context, blockContext *smblock.Context) error {
	timestamp := b.Stateless.Time()
	if time.Until(timestamp) > maxClockSkew {
		return errFutureTimestamp
	}

	// parent block must be verified or accepted
	parent, exists := b.chain.verifiedBlocks[b.ParentID]
	if !exists {
		return errMissingParent
	}

	if b.Stateless.Height != parent.Stateless.Height+1 {
		return errWrongHeight
	}

	parentTimestamp := parent.Stateless.Time()
	if timestamp.Before(parentTimestamp) {
		return errTimestampBeforeParent
	}

	parentState, err := parent.State()
	if err != nil {
		return err
	}

	// This block's state is a versionDB built on top of it's parent state. This
	// block's changes are pushed atomically to the parent state when accepted.
	blkState := versiondb.New(parentState)
	err = execute.Block(
		ctx,
		b.chain.chainContext,
		blkState,
		b.chain.chainState == quasar.Bootstrapping,
		blockContext,
		b.Stateless,
	)
	if err != nil {
		return err
	}

	// Make sure to only state the state the first time we verify this block.
	if b.state == nil {
		b.state = blkState
		parent.verifiedChildrenIDs.Add(b.id)
		b.chain.verifiedBlocks[b.id] = b
	}

	return nil
}

func (b *block) State() (db.Database, error) {
	if b.id == b.chain.lastAcceptedID {
		return b.chain.acceptedState, nil
	}

	// If this block isn't processing, then the child should never have had
	// verify called on it.
	if b.state == nil {
		return nil, errParentNotVerified
	}

	return b.state, nil
}

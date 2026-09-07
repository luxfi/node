// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chains/atomic"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
)

type proposalBlockState struct {
	onDecisionState state.Diff
	onCommitState   state.Diff
	onAbortState    state.Diff
}

// The state of a block.
// Note that not all fields will be set for a given block.
type blockState struct {
	proposalBlockState
	statelessBlock block.Block

	onAcceptState state.Diff
	onAcceptFunc  func()

	inputs         set.Set[ids.ID]
	timestamp      time.Time
	atomicRequests map[ids.ID]*atomic.Requests
	metrics        metrics.Block

	// verifiedHeights is the ONLY field of this struct touched from more than
	// one goroutine, and it must be reached through the two accessors below.
	//
	// backend.blkIDToStateLock guards the blkIDToState MAP; it says nothing
	// about the struct that map hands out. chainRouter.HandleInbound starts a
	// goroutine per inbound message, so two blocks can sit in
	// VerifyWithRuntime at once sharing one *blockState — one reading
	// Contains, the other writing Add. set.Set is a bare map, so that is not a
	// lost update, it is `fatal error: concurrent map read and map write`: the
	// runtime aborts the process and recover() never sees it.
	verifiedHeightsLock sync.RWMutex
	verifiedHeights     set.Set[uint64]
}

// hasVerifiedHeight reports whether the block's warp messages were already
// verified at this P-Chain height.
func (b *blockState) hasVerifiedHeight(height uint64) bool {
	b.verifiedHeightsLock.RLock()
	defer b.verifiedHeightsLock.RUnlock()
	return b.verifiedHeights.Contains(height)
}

// markVerifiedHeight records that the block's warp messages are valid at this
// P-Chain height.
func (b *blockState) markVerifiedHeight(height uint64) {
	b.verifiedHeightsLock.Lock()
	defer b.verifiedHeightsLock.Unlock()
	b.verifiedHeights.Add(height)
}

// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/platformvm/block"
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
}

// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/luxdefi/node/chains/atomic"
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/utils/set"
	"github.com/luxdefi/node/vms/platformvm/block"
	"github.com/luxdefi/node/vms/platformvm/state"
)

type standardBlockState struct {
	onAcceptFunc func()
	inputs       set.Set[ids.ID]
}

type proposalBlockState struct {
	initiallyPreferCommit bool
	onCommitState         state.Diff
	onAbortState          state.Diff
}

// The state of a block.
// Note that not all fields will be set for a given block.
type blockState struct {
	standardBlockState
	proposalBlockState
	statelessBlock block.Block
	onAcceptState  state.Diff

	timestamp      time.Time
	atomicRequests map[ids.ID]*atomic.Requests
}

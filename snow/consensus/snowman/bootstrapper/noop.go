// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
)

var Noop Poll = noop{}

type noop struct{}

func (noop) GetPeers(context.Context) set.Set[ids.NodeID] {
	return nil
}

func (noop) RecordOpinion(context.Context, ids.NodeID, set.Set[ids.ID]) error {
	return nil
}

func (noop) Result(context.Context) ([]ids.ID, bool) {
	return nil, false
}

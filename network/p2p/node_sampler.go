// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/node/ids"
)

// NodeSampler samples nodes in network
type NodeSampler interface {
	// Sample returns at most [limit] nodes. This may return fewer nodes if
	// fewer than [limit] are available.
	Sample(ctx context.Context, limit int) []ids.NodeID
}

// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"

	"github.com/luxfi/consensus/core"
)

// Registrant can register the existence of a chain
type Registrant interface {
	// Called when a chain is created
	// This function is called before the chain starts processing messages
	// [vm] should be a vertex.GRAPHVM or block.ChainVM
	RegisterChain(chainName string, ctx context.Context, vm core.VM)
}

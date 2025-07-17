// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import "github.com/luxfi/node/ids"

// Benchable is notified when a validator is benched or unbenched from a given chain
type Benchable interface {
	// Mark that [validatorID] has been benched on the given chain
	Benched(chainID ids.ID, validatorID ids.NodeID)
	// Mark that [validatorID] has been unbenched from the given chain
	Unbenched(chainID ids.ID, validatorID ids.NodeID)
}

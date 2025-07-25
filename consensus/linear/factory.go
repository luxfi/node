// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linear

// Factory returns new instances of Consensus
type Factory interface {
	New() Consensus
}

// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

// Factory returns new instances of Consensus
type Factory interface {
	New() Consensus
}

// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package fx

// Factory returns an instance of a feature extension
type Factory interface {
	New() any
}

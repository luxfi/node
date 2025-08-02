// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fx

// Factory returns an instance of a feature extension
type Factory interface {
	New() any
}

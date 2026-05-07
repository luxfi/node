// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import "context"

// RuntimeInitializable can be initialized with a context
type RuntimeInitializable interface {
	InitRuntime(context.Context)
}

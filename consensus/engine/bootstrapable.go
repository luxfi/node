// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import "context"

type BootstrapableEngine interface {
	Engine

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}

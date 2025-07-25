// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import "context"

type BootstrapableEngine interface {
	Engine

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}

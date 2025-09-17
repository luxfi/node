// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import "errors"

var (
	// ErrNotOracle is returned when a block doesn't have options
	ErrNotOracle = errors.New("block doesn't have options")
)
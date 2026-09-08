// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import "errors"

var (
	ErrInvalidBlockHeight = errors.New("blockdb: invalid block height")
	ErrBlockEmpty         = errors.New("blockdb: block is empty")
	ErrDatabaseClosed     = errors.New("blockdb: database is closed")
	ErrCorrupted          = errors.New("blockdb: unrecoverable corruption detected")
	ErrBlockTooLarge      = errors.New("blockdb: block size too large")
	ErrBlockNotFound      = errors.New("blockdb: block not found")
)

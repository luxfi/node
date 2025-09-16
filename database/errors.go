// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"errors"

	luxdb "github.com/luxfi/database"
)

// common errors
var (
	ErrClosed   = errors.New("closed")
	ErrNotFound = luxdb.ErrNotFound
)

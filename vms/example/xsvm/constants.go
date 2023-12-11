// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/version"
)

const Name = "xsvm"

var (
	ID = ids.ID{'x', 's', 'v', 'm'}

	Version = &version.Semantic{
		Major: 1,
		Minor: 0,
		Patch: 4,
	}
)

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	// ID that this Fx uses when labeled
	ID = ids.ID{'s', 'e', 'c', 'p', '2', '5', '6', 'k', '1', 'f', 'x'}
)

type Factory struct{}

<<<<<<< HEAD
func (*Factory) New(*snow.Context) (interface{}, error) {
	return &Fx{}, nil
}
=======
func (*Factory) New(*snow.Context) (interface{}, error) { return &Fx{}, nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))

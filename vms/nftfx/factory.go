// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	// ID that this Fx uses when labeled
	ID = ids.ID{'n', 'f', 't', 'f', 'x'}
)

type Factory struct{}

<<<<<<< HEAD
<<<<<<< HEAD
func (*Factory) New(*snow.Context) (interface{}, error) {
	return &Fx{}, nil
}
=======
func (*Factory) New(*snow.Context) (interface{}, error) { return &Fx{}, nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
=======
func (*Factory) New(*snow.Context) (interface{}, error) {
	return &Fx{}, nil
}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))

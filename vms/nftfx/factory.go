// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	// ID that this Fx uses when labeled
	ID = ids.ID{'n', 'f', 't', 'f', 'x'}
)

type Factory struct{}

func (*Factory) New(logging.Logger) (interface{}, error) {
	return &Fx{}, nil
}

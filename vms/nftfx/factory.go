// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/fx"
)

const Name = "nftfx"

var (
	_ fx.Factory = (*Factory)(nil)

	// ID that this Fx uses when labeled
	ID = ids.ID{'n', 'f', 't', 'f', 'x'}
)

type Factory struct{}

func (*Factory) New() any {
	return &Fx{}
}

// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"reflect"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/codec"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/vms/xvm/config"
	"github.com/luxfi/node/v2/vms/xvm/fxs"
)

type Backend struct {
	Ctx           *quasar.Context
	Config        *config.Config
	Fxs           []*fxs.ParsedFx
	TypeToFxIndex map[reflect.Type]int
	Codec         codec.Manager
	// Note: FeeAssetID may be different than ctx.LUXAssetID if this XVM is
	// running in a subnet.
	FeeAssetID   ids.ID
	Bootstrapped bool
}

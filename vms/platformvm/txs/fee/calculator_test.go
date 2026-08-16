// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/luxfi/constants"
	"github.com/luxfi/node/vms/components/gas"
)

const testDynamicPrice = gas.Price(constants.NanoLux)

var testDynamicWeights = gas.Dimensions{
	gas.Bandwidth: 1,
	gas.DBRead:    2000,
	gas.DBWrite:   20000,
	gas.Compute:   10,
}

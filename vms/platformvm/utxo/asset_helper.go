// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import "github.com/luxfi/ids"

// UTXOAssetID is the LUX asset ID. It is set during node initialization
// from the genesis configuration via the VM context.
var UTXOAssetID ids.ID

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import "github.com/luxfi/node/proto/zap/platformvm"

type (
	L1ValidatorRegistrationJustification                            = platformvm.L1ValidatorRegistrationJustification
	L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData     = platformvm.L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData
	L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage = platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage
	L1ValidatorWeightJustification                                  = platformvm.L1ValidatorWeightJustification
	L1ValidatorWeightJustification_L1ValidatorWeightMessage         = platformvm.L1ValidatorWeightJustification_L1ValidatorWeightMessage
	ChainIDIndex                                                    = platformvm.ChainIDIndex
)

// Unmarshal decodes a message from bytes. The ZAP codec encodes
// messages via direct field assignment; this entry point is reserved
// for compatibility with callers that thread a generic byte payload
// through the wire types and is a no-op until a concrete consumer
// arrives.
func Unmarshal(data []byte, m interface{}) error {
	return nil
}

//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import "github.com/luxfi/node/proto/zap/platformvm"

type (
	L1ValidatorRegistrationJustification                            = platformvm.L1ValidatorRegistrationJustification
	L1ValidatorRegistrationJustification_ConvertChainToL1TxData     = platformvm.L1ValidatorRegistrationJustification_ConvertChainToL1TxData
	L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage = platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage
	L1ValidatorWeightJustification                                  = platformvm.L1ValidatorWeightJustification
	L1ValidatorWeightJustification_L1ValidatorWeightMessage         = platformvm.L1ValidatorWeightJustification_L1ValidatorWeightMessage
	ChainIDIndex                                                    = platformvm.ChainIDIndex
)

// Unmarshal decodes a message from bytes (ZAP implementation)
func Unmarshal(data []byte, m interface{}) error {
	// ZAP uses direct field assignment - for now just return nil
	// Actual ZAP unmarshaling would use a codec
	return nil
}

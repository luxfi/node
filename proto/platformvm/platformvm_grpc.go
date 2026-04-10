//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/luxfi/node/proto/pb/platformvm"
	"google.golang.org/protobuf/proto"
)

type (
	L1ValidatorRegistrationJustification                            = platformvm.L1ValidatorRegistrationJustification
	L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData     = platformvm.L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData
	L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage = platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage
	ChainIDIndex                                                    = platformvm.ChainIDIndex
)

// Unmarshal decodes a message from bytes (protobuf implementation)
func Unmarshal(data []byte, m proto.Message) error {
	return proto.Unmarshal(data, m)
}

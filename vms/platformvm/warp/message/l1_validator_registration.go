// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// L1ValidatorRegistration reports if a validator is registered on the P-chain.
//
// Wire: mkind u8 @0, ValidationID 32B @1, Registered u8 @33 = 34 bytes.
type L1ValidatorRegistration struct {
	payload

	ValidationID ids.ID `json:"validationID"`
	// Registered being true means that validationID is currently a validator on
	// the P-chain.
	//
	// Registered being false means that validationID is not and can never
	// become a validator on the P-chain. It is possible that validationID was
	// previously a validator on the P-chain.
	Registered bool `json:"registered"`
}

const (
	regOffValidationID = 1
	regOffRegistered   = 33
	regSize            = 34
)

// NewL1ValidatorRegistration creates a new initialized L1ValidatorRegistration.
func NewL1ValidatorRegistration(
	validationID ids.ID,
	registered bool,
) (*L1ValidatorRegistration, error) {
	msg := &L1ValidatorRegistration{
		ValidationID: validationID,
		Registered:   registered,
	}
	b := zap.NewBuilder(zap.HeaderSize + regSize)
	ob := b.StartObject(regSize)
	ob.SetUint8(offMKind, uint8(mkindL1ValidatorRegistration))
	ob.SetBytesFixed(regOffValidationID, validationID[:])
	var reg uint8
	if registered {
		reg = 1
	}
	ob.SetUint8(regOffRegistered, reg)
	ob.FinishAsRoot()
	msg.initialize(b.Finish())
	return msg, nil
}

func parseL1ValidatorRegistration(root zap.Object) *L1ValidatorRegistration {
	return &L1ValidatorRegistration{
		ValidationID: readID(root, regOffValidationID),
		Registered:   root.Uint8(regOffRegistered) != 0,
	}
}

// ParseL1ValidatorRegistration parses bytes into an initialized
// L1ValidatorRegistration.
func ParseL1ValidatorRegistration(b []byte) (*L1ValidatorRegistration, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*L1ValidatorRegistration)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"
	"math"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var ErrNonceReservedForRemoval = errors.New("maxUint64 nonce is reserved for removal")

// L1ValidatorWeight is both received and sent by the P-chain.
//
// If the P-chain is receiving this message, it is treated as a command to
// update the weight of the validator.
//
// If the P-chain is sending this message, it is reporting the current nonce and
// weight of the validator.
//
// Wire: mkind u8 @0, ValidationID 32B @1, Nonce u64 @33, Weight u64 @41 = 49B.
type L1ValidatorWeight struct {
	payload

	ValidationID ids.ID `json:"validationID"`
	Nonce        uint64 `json:"nonce"`
	Weight       uint64 `json:"weight"`
}

const (
	vwOffValidationID = 1
	vwOffNonce        = 33
	vwOffWeight       = 41
	vwSize            = 49
)

func (s *L1ValidatorWeight) Verify() error {
	if s.Nonce == math.MaxUint64 && s.Weight != 0 {
		return ErrNonceReservedForRemoval
	}
	return nil
}

// NewL1ValidatorWeight creates a new initialized L1ValidatorWeight.
func NewL1ValidatorWeight(
	validationID ids.ID,
	nonce uint64,
	weight uint64,
) (*L1ValidatorWeight, error) {
	msg := &L1ValidatorWeight{
		ValidationID: validationID,
		Nonce:        nonce,
		Weight:       weight,
	}
	b := zap.NewBuilder(zap.HeaderSize + vwSize)
	ob := b.StartObject(vwSize)
	ob.SetUint8(offMKind, uint8(mkindL1ValidatorWeight))
	ob.SetBytesFixed(vwOffValidationID, validationID[:])
	ob.SetUint64(vwOffNonce, nonce)
	ob.SetUint64(vwOffWeight, weight)
	ob.FinishAsRoot()
	msg.initialize(b.Finish())
	return msg, nil
}

func parseL1ValidatorWeight(root zap.Object) *L1ValidatorWeight {
	return &L1ValidatorWeight{
		ValidationID: readID(root, vwOffValidationID),
		Nonce:        root.Uint64(vwOffNonce),
		Weight:       root.Uint64(vwOffWeight),
	}
}

// ParseL1ValidatorWeight parses bytes into an initialized L1ValidatorWeight.
func ParseL1ValidatorWeight(b []byte) (*L1ValidatorWeight, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*L1ValidatorWeight)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

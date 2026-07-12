// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/luxfi/zap"
)

var _ Payload = (*AddressedCall)(nil)

// AddressedCall wire: pkind u8 @0, SourceAddress bytes-ptr @1, Payload
// bytes-ptr @9 = 17-byte object header (+ the two byte blobs in the buffer).
const (
	acOffKind    = offPKind
	acOffSource  = 1
	acOffPayload = 9
	acSize       = 17
)

// AddressedCall defines the format for delivering a call across VMs including a
// source address and a payload.
//
// Note: If a destination address is expected, it should be encoded in the
// payload.
type AddressedCall struct {
	SourceAddress []byte `json:"sourceAddress"`
	Payload       []byte `json:"payload"`

	bytes []byte
}

// NewAddressedCall creates a new *AddressedCall and initializes it.
func NewAddressedCall(sourceAddress []byte, payload []byte) (*AddressedCall, error) {
	ap := &AddressedCall{
		SourceAddress: sourceAddress,
		Payload:       payload,
	}
	b := zap.NewBuilder(zap.HeaderSize + acSize + len(sourceAddress) + len(payload) + 64)
	ob := b.StartObject(acSize)
	ob.SetUint8(acOffKind, uint8(pkindAddressedCall))
	ob.SetBytes(acOffSource, sourceAddress)
	ob.SetBytes(acOffPayload, payload)
	ob.FinishAsRoot()
	ap.initialize(b.Finish())
	return ap, nil
}

// ParseAddressedCall converts a slice of bytes into an initialized
// AddressedCall.
func ParseAddressedCall(b []byte) (*AddressedCall, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*AddressedCall)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewAddressedCall or Parse.
func (a *AddressedCall) Bytes() []byte {
	return a.bytes
}

func (a *AddressedCall) initialize(bytes []byte) {
	a.bytes = bytes
}

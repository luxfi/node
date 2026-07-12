// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var ErrWrongType = errors.New("wrong payload type")

// Payload provides a common interface for all payloads implemented by this
// package. Each payload is a native-ZAP struct-is-wire object whose first byte
// is a pkind discriminator (the struct IS the buffer; no codec).
type Payload interface {
	// Bytes returns the binary representation of this payload.
	Bytes() []byte

	// initialize the payload with the provided binary representation.
	initialize(b []byte)
}

// pkind is the 1-byte payload discriminator at object offset 0. The values are
// the on-wire type ids (formerly the linearcodec registration order): Hash=0,
// AddressedCall=1. Appending a new payload appends a new id — never reorder.
type pkind uint8

const (
	pkindHash          pkind = 0
	pkindAddressedCall pkind = 1

	offPKind = 0
)

func readID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, 32))
	return id
}

// Parse dispatches a payload buffer on its pkind discriminator.
func Parse(bytes []byte) (Payload, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	var p Payload
	switch k := pkind(root.Uint8(offPKind)); k {
	case pkindHash:
		p = &Hash{Hash: readID(root, hashOffHash)}
	case pkindAddressedCall:
		p = &AddressedCall{
			SourceAddress: append([]byte(nil), root.Bytes(acOffSource)...),
			Payload:       append([]byte(nil), root.Bytes(acOffPayload)...),
		}
	default:
		return nil, fmt.Errorf("%w: unknown pkind %d", ErrWrongType, k)
	}
	p.initialize(bytes)
	return p, nil
}

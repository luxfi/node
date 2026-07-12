// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var ErrWrongType = errors.New("wrong payload type")

// Payload provides a common interface for all payloads implemented by this
// package. Each payload is a native-ZAP struct-is-wire object whose first byte
// is an mkind discriminator (the struct IS the buffer; no codec).
type Payload interface {
	// Bytes returns the binary representation of this payload.
	//
	// If the payload is not initialized, this method will return nil.
	Bytes() []byte

	// initialize the payload with the provided binary representation.
	initialize(b []byte)
}

// payload is embedded by all the payloads to provide the common implementation
// of Payload.
type payload []byte

func (p payload) Bytes() []byte {
	return p
}

func (p *payload) initialize(bytes []byte) {
	*p = bytes
}

// mkind is the 1-byte message-payload discriminator at object offset 0. The
// values are the on-wire type ids (formerly the linearcodec registration
// order): ChainToL1Conversion=0, RegisterL1Validator=1,
// L1ValidatorRegistration=2, L1ValidatorWeight=3. Appending a new payload
// appends a new id — never reorder.
type mkind uint8

const (
	mkindChainToL1Conversion     mkind = 0
	mkindRegisterL1Validator     mkind = 1
	mkindL1ValidatorRegistration mkind = 2
	mkindL1ValidatorWeight       mkind = 3

	offMKind = 0
)

func readID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, 32))
	return id
}

// Parse dispatches a payload buffer on its mkind discriminator.
func Parse(bytes []byte) (Payload, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	var p Payload
	switch k := mkind(root.Uint8(offMKind)); k {
	case mkindChainToL1Conversion:
		p = parseChainToL1Conversion(root)
	case mkindRegisterL1Validator:
		p = parseRegisterL1Validator(root)
	case mkindL1ValidatorRegistration:
		p = parseL1ValidatorRegistration(root)
	case mkindL1ValidatorWeight:
		p = parseL1ValidatorWeight(root)
	default:
		return nil, fmt.Errorf("%w: unknown mkind %d", ErrWrongType, k)
	}
	p.initialize(bytes)
	return p, nil
}

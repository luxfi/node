// Copyright (C) 2023-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

type Unsigned interface {
	Visit(Visitor) error

	// Marshal encodes the unsigned tx to its native ZAP wire form. The first
	// object byte is the tx kind discriminator — this is the whole dispatch,
	// no codec.
	Marshal() ([]byte, error)
}

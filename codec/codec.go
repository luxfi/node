// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import "github.com/luxdefi/luxd/utils/wrappers"

// Codec marshals and unmarshals
type Codec interface {
	MarshalInto(interface{}, *wrappers.Packer) error
	Unmarshal([]byte, interface{}) error

	// Returns the size, in bytes, of [value] when it's marshaled
	Size(value interface{}) (int, error)
}

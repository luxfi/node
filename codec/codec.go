// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import "github.com/luxdefi/luxd/utils/wrappers"

// Codec marshals and unmarshals
type Codec interface {
	MarshalInto(interface{}, *wrappers.Packer) error
	Unmarshal([]byte, interface{}) error
}

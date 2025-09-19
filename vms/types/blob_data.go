// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"encoding/json"

	"github.com/luxfi/node/utils/formatting"
)

// JSONByteSlice represents [[]byte] that is json marshalled to hex
type JSONByteSlice []byte

func (b JSONByteSlice) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	hexData, err := formatting.Encode(formatting.HexNC, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(hexData)
}

func (b *JSONByteSlice) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		// Keep the existing value when unmarshaling null
		return nil
	}

	var hexData string
	if err := json.Unmarshal(data, &hexData); err != nil {
		return err
	}

	decoded, err := formatting.Decode(formatting.HexNC, hexData)
	if err != nil {
		return err
	}

	*b = decoded
	return nil
}

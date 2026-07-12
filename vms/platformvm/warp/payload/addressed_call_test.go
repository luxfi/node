// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

func TestAddressedCall(t *testing.T) {
	require := require.New(t)
	shortID := ids.GenerateTestShortID()

	addressedPayload, err := NewAddressedCall(
		shortID[:],
		[]byte{1, 2, 3},
	)
	require.NoError(err)

	addressedPayloadBytes := addressedPayload.Bytes()
	parsedAddressedPayload, err := ParseAddressedCall(addressedPayloadBytes)
	require.NoError(err)
	require.Equal(addressedPayload, parsedAddressedPayload)
}

func TestParseAddressedCallJunk(t *testing.T) {
	_, err := ParseAddressedCall(junkBytes)
	require.ErrorIs(t, err, zap.ErrBufferTooSmall)
}

// TestAddressedCallBytes pins the native-ZAP wire (golden generated from the
// struct-is-wire encoder at cutover; re-genesis retired the codec wire). Any
// future encoder change that shifts these bytes is a wire break — fail loudly.
func TestAddressedCallBytes(t *testing.T) {
	require := require.New(t)
	base64Payload := "WkFQAAIAAAAQAAAANAAAAAEQAAAAEAAAABgAAAADAAAAAQIDAAAAAAAAAAAAAAAAAAoLDA=="
	addressedPayload, err := NewAddressedCall(
		[]byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		[]byte{10, 11, 12},
	)
	require.NoError(err)
	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(addressedPayload.Bytes()))
	// structural pin: pkind discriminator sits at the first object byte.
	require.Equal(uint8(pkindAddressedCall), addressedPayload.Bytes()[zap.HeaderSize])
}

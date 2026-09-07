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

func TestHash(t *testing.T) {
	require := require.New(t)

	hashPayload, err := NewHash(ids.GenerateTestID())
	require.NoError(err)

	hashPayloadBytes := hashPayload.Bytes()
	parsedHashPayload, err := ParseHash(hashPayloadBytes)
	require.NoError(err)
	require.Equal(hashPayload, parsedHashPayload)
}

func TestParseHashJunk(t *testing.T) {
	_, err := ParseHash(junkBytes)
	require.ErrorIs(t, err, zap.ErrBufferTooSmall)
}

// TestHashBytes pins the native-ZAP wire (golden generated from the
// struct-is-wire encoder at cutover; re-genesis retired the codec wire). Any
// future encoder change that shifts these bytes is a wire break — fail loudly.
func TestHashBytes(t *testing.T) {
	require := require.New(t)
	base64Payload := "WkFQAAIAAAAQAAAAMQAAAAAEBQYAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	hashPayload, err := NewHash(ids.ID{4, 5, 6})
	require.NoError(err)
	require.Equal(base64Payload, base64.StdEncoding.EncodeToString(hashPayload.Bytes()))
	// structural pin: pkind discriminator sits at the first object byte.
	require.Equal(uint8(pkindHash), hashPayload.Bytes()[zap.HeaderSize])
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

func TestMessage(t *testing.T) {
	require := require.New(t)

	payload := []byte("payload")

	unsignedMsg, err := NewUnsignedMessage(
		constants.UnitTestID,
		ids.GenerateTestID(),
		payload,
	)
	require.NoError(err)
	require.Len(unsignedMsg.Bytes(), zap.HeaderSize+umSize+len(payload)) // header + object (incl. inline bytes-ptr) + payload blob

	msg, err := NewMessage(
		unsignedMsg,
		&BitSetSignature{
			Signers:   []byte{1, 2, 3},
			Signature: [bls.SignatureLen]byte{4, 5, 6},
		},
	)
	require.NoError(err)

	msgBytes := msg.Bytes()
	msg2, err := ParseMessage(msgBytes)
	require.NoError(err)
	require.Equal(msg, msg2)
}

func TestParseMessageJunk(t *testing.T) {
	require := require.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	_, err := ParseMessage(bytes)
	require.ErrorIs(err, zap.ErrBufferTooSmall)
}

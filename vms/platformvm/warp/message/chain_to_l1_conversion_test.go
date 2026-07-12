// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm/types"
	"github.com/luxfi/zap"
)

func TestChainToL1ConversionID(t *testing.T) {
	require := require.New(t)

	// The pinned native-ZAP hash preimage (golden generated from the
	// struct-is-wire encoder at cutover; re-genesis retired the codec wire).
	// The conversion ID is verified by the L1 staking contract, so this
	// preimage IS consensus surface: any encoder change that shifts these
	// bytes changes every conversion ID — fail loudly.
	chainToL1ConversionDataBytes, err := base64.StdEncoding.DecodeString(
		"WkFQAAIAAABQAAAAvQAAAAAAAAAUAAAAVldYWVpbXF1eX2BhYmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5ent8fX5/gIGCg4SFjYyLiomIh4YBAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj9AGAAAAAEAAAB4////AQAAAAkAAAAUAAAAQUJDREVGR0hJSktMTU5PUFFSU1RV",
	)
	require.NoError(err)
	var expectedChainToL1ConversionID ids.ID = hash.ComputeHash256Array(chainToL1ConversionDataBytes)

	chainToL1ConversionData := ChainToL1ConversionData{
		ChainID: ids.ID{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
		},
		ManagerChainID: ids.ID{
			0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30,
			0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
			0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40,
		},
		ManagerAddress: []byte{0x41},
		Validators: []ChainToL1ConversionValidatorData{
			{
				NodeID: types.JSONByteSlice([]byte{
					0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
					0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50, 0x51,
					0x52, 0x53, 0x54, 0x55,
				}),
				BLSPublicKey: [bls.PublicKeyLen]byte{
					0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d,
					0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65,
					0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d,
					0x6e, 0x6f, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75,
					0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d,
					0x7e, 0x7f, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85,
				},
				Weight: 0x868788898a8b8c8d,
			},
		},
	}
	// Marshal must reproduce the pinned preimage byte-for-byte…
	marshaled, err := chainToL1ConversionData.Marshal()
	require.NoError(err)
	require.Equal(chainToL1ConversionDataBytes, marshaled)
	// …with the fixed fields at their documented object offsets (object starts
	// right after the trailing side arrays; ChainID @+0, ManagerChainID @+32).
	objStart := len(marshaled) - 21 /* pools */ - cdSize
	require.Equal(chainToL1ConversionData.ChainID[:], marshaled[objStart+cdOffChainID:objStart+cdOffChainID+32])
	require.Equal(chainToL1ConversionData.ManagerChainID[:], marshaled[objStart+cdOffManagerID:objStart+cdOffManagerID+32])

	// The ID is sha256 of exactly that preimage — ID and Marshal share ONE
	// encoder and can never diverge.
	chainToL1ConversionID, err := ChainToL1ConversionID(chainToL1ConversionData)
	require.NoError(err)
	require.Equal(expectedChainToL1ConversionID, chainToL1ConversionID)

	// sanity: the buffer is a well-formed zap message.
	_, err = zap.Parse(marshaled)
	require.NoError(err)
}

func TestChainToL1Conversion(t *testing.T) {
	require := require.New(t)

	msg, err := NewChainToL1Conversion(ids.GenerateTestID())
	require.NoError(err)

	parsed, err := ParseChainToL1Conversion(msg.Bytes())
	require.NoError(err)
	require.Equal(msg, parsed)
}

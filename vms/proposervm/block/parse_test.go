// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/zap"
)

func TestParseBlocks(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		Epoch{},
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)

	signedBlockBytes := signedBlock.Bytes()
	malformedBlockBytes := make([]byte, len(signedBlockBytes)-1)
	copy(malformedBlockBytes, signedBlockBytes)

	for _, testCase := range []struct {
		name   string
		input  [][]byte
		output []ParseResult
	}{
		{
			name:   "ValidThenInvalid",
			input:  [][]byte{signedBlockBytes, malformedBlockBytes},
			output: []ParseResult{{Block: &statelessBlock{bytes: signedBlockBytes}}, {Err: zap.ErrBufferTooSmall}},
		},
		{
			name:   "InvalidThenValid",
			input:  [][]byte{malformedBlockBytes, signedBlockBytes},
			output: []ParseResult{{Err: zap.ErrBufferTooSmall}, {Block: &statelessBlock{bytes: signedBlockBytes}}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			results := ParseBlocks(testCase.input, chainID)
			for i := range testCase.output {
				if testCase.output[i].Block == nil {
					require.Nil(t, results[i].Block)
					require.ErrorIs(t, results[i].Err, testCase.output[i].Err)
				} else {
					require.Equal(t, testCase.output[i].Block.Bytes(), results[i].Block.Bytes())
					require.NoError(t, results[i].Err)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		Epoch{},
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)

	unsignedBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, Epoch{}, innerBlockBytes)
	require.NoError(t, err)

	// A block that carries no certificate but does carry a signature suffix:
	// verify must reject it with errUnexpectedSignature.
	signedWithoutCertBlockIntf, err := BuildUnsigned(parentID, timestamp, pChainHeight, Epoch{}, innerBlockBytes)
	require.NoError(t, err)
	signedWithoutCertBlock := signedWithoutCertBlockIntf.(*statelessBlock)
	require.NoError(t, signedWithoutCertBlock.initialize(concat(signedWithoutCertBlock.Bytes(), buildSigBuffer([]byte{5}))))

	optionBlock, err := BuildOption(parentID, innerBlockBytes)
	require.NoError(t, err)

	tests := []struct {
		name        string
		block       Block
		chainID     ids.ID
		expectedErr error
	}{
		{
			name:        "correct chainID",
			block:       signedBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "invalid chainID",
			block:       signedBlock,
			chainID:     ids.ID{5},
			expectedErr: staking.ErrECDSAVerificationFailure,
		},
		{
			name:        "unsigned block",
			block:       unsignedBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "invalid signature",
			block:       signedWithoutCertBlockIntf,
			chainID:     chainID,
			expectedErr: errUnexpectedSignature,
		},
		{
			name:        "option block",
			block:       optionBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			blockBytes := test.block.Bytes()
			parsedBlockWithoutVerification, err := ParseWithoutVerification(blockBytes)
			require.NoError(err)
			equal(require, test.block, parsedBlockWithoutVerification)

			parsedBlock, err := Parse(blockBytes, test.chainID)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				equal(require, test.block, parsedBlock)
			}
		})
	}
}

func TestParseBytes(t *testing.T) {
	chainID := ids.ID{4}
	tests := []struct {
		name        string
		hex         string
		expectedErr error
	}{
		{
			// Fewer than zap.HeaderSize bytes: rejected at the wire boundary.
			name:        "too short",
			hex:         "000102030405",
			expectedErr: zap.ErrBufferTooSmall,
		},
		{
			// A 16-byte frame with a valid size field (=16) but the wrong magic
			// bytes: rejected by zap.Parse.
			name:        "invalid magic",
			hex:         "00000000000000000000000010000000",
			expectedErr: zap.ErrInvalidMagic,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			bytes, err := hex.DecodeString(test.hex)
			require.NoError(err)

			_, err = Parse(bytes, chainID)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestParseHeader(t *testing.T) {
	require := require.New(t)

	chainID := ids.ID{1}
	parentID := ids.ID{2}
	bodyID := ids.ID{3}

	builtHeader, err := BuildHeader(
		chainID,
		parentID,
		bodyID,
	)
	require.NoError(err)

	builtHeaderBytes := builtHeader.Bytes()

	parsedHeader, err := ParseHeader(builtHeaderBytes)
	require.NoError(err)

	equalHeader(require, builtHeader, parsedHeader)
}

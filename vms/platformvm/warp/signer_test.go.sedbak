// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"
	

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/bls"
)

func TestSigner(t *testing.T) {
	signerTests := map[string]func(t *testing.T, s Signer, sk *bls.SecretKey, networkID uint32, chainID ids.ID){
		"WrongChainID":   testWrongChainID,
		"WrongNetworkID": testWrongNetworkID,
		"Verifies":       testVerifies,
	}

	for name, test := range signerTests {
		t.Run(name, func(t *testing.T) {
			sk, err := bls.NewSecretKey()
			require.NoError(t, err)

			chainID := ids.GenerateTestID()
			s := NewSigner(sk, constants.UnitTestID, chainID)

			test(t, s, sk, constants.UnitTestID, chainID)
		})
	}
}

// Test that using a random SourceChainID results in an error
func testWrongChainID(t *testing.T, s Signer, _ *bls.SecretKey, _ uint32, _ ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		constants.UnitTestID,
		ids.GenerateTestID(),
		[]byte("payload"),
	)
	require.NoError(err)

	_, err = s.Sign(msg)
	// TODO: require error to be ErrWrongSourceChainID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

// Test that using a different networkID results in an error
func testWrongNetworkID(t *testing.T, s Signer, _ *bls.SecretKey, networkID uint32, blockchainID ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		networkID+1,
		blockchainID,
		[]byte("payload"),
	)
	require.NoError(err)

	_, err = s.Sign(msg)
	// TODO: require error to be ErrWrongNetworkID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

// Test that a signature generated with the signer verifies correctly
func testVerifies(t *testing.T, s Signer, sk *bls.SecretKey, networkID uint32, chainID ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		networkID,
		chainID,
		[]byte("payload"),
	)
	require.NoError(err)

	sigBytes, err := s.Sign(msg)
	require.NoError(err)

	sig, err := bls.SignatureFromBytes(sigBytes)
	require.NoError(err)

	pk := bls.PublicFromSecretKey(sk)
	msgBytes := msg.Bytes()
	require.True(bls.Verify(pk, sig, msgBytes))
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleporter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestSigner(t *testing.T) {
<<<<<<< HEAD
	for _, test := range SignerTests {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)
=======
	require := require.New(t)

	for _, test := range SignerTests {
		sk, err := bls.NewSecretKey()
		require.NoError(err)
>>>>>>> 978209904 (Add Teleporter message signing to snow.Context (#2197))

		chainID := ids.GenerateTestID()
		s := NewSigner(sk, chainID)

		test(t, s, sk, chainID)
	}
}

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
=======
	for _, test := range SignerTests {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)
>>>>>>> 7c09e7074 (Standardize `require` usage and remove `t.Fatal` from platformvm (#2297))

		chainID := ids.GenerateTestID()
		s := NewSigner(sk, chainID)

		test(t, s, sk, chainID)
	}
}

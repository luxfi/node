// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls"
)

func TestSigner(t *testing.T) {
	for _, test := range SignerTests {
		sk, err := bls.NewSecretKey()
		require.NoError(t, err)

		chainID := ids.GenerateTestID()
		s := NewSigner(sk, constants.UnitTestID, chainID)

		test(t, s, sk, constants.UnitTestID, chainID)
	}
}

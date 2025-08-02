// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms/platformvm/warp"
	"github.com/luxfi/node/v2/vms/platformvm/warp/signertest"
)

func TestSigner(t *testing.T) {
	for name, test := range signertest.SignerTests {
		t.Run(name, func(t *testing.T) {
			sk, err := localsigner.New()
			require.NoError(t, err)

			chainID := ids.GenerateTestID()
			s := warp.NewSigner(sk, constants.UnitTestID, chainID)

			test(t, s, sk, constants.UnitTestID, chainID)
		})
	}
}

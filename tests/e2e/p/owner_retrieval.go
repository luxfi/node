// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package p

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var _ = e2e.DescribePChain("[P-Chain Wallet]", func() {
	tc := e2e.NewTestContext()
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should support retrieving net owners", func() {
		env := e2e.Env

		nodeURI := env.GetRandomNodeURI()
		pChainClient := platformvm.NewClient(nodeURI.URI)

		keychain := env.NewKeychain(1)
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		pWallet := baseWallet.P()

		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		}

		tc.By("creating a permissioned subnet")
		subnetTx, err := pWallet.IssueCreateNetTx(
			owner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)
		netID := subnetTx.ID()
		require.NotEqual(netID, constants.PrimaryNetworkID)

		tc.By("verifying owner", func() {
			subnetResponse, err := pChainClient.GetSubnet(
				tc.DefaultContext(),
				netID,
			)
			require.NoError(err)
			require.True(subnetResponse.IsPermissioned)
			require.Equal(owner.Threshold, subnetResponse.Threshold)
			require.Equal(owner.Addrs, subnetResponse.ControlKeys)
		})

		newOwnerKey := env.AllocatePreFundedKey()
		newOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				newOwnerKey.Address(),
			},
		}

		tc.By("changing net owner")
		_, err = pWallet.IssueTransferNetOwnershipTx(
			netID,
			newOwner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.By("verifying new owner", func() {
			subnetResponse, err := pChainClient.GetSubnet(
				tc.DefaultContext(),
				netID,
			)
			require.NoError(err)
			require.True(subnetResponse.IsPermissioned)
			require.Equal(newOwner.Threshold, subnetResponse.Threshold)
			require.Equal(newOwner.Addrs, subnetResponse.ControlKeys)
		})

		e2e.CheckBootstrapIsPossible(env.GetNetwork())
	})
})

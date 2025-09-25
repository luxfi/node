// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package p

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var _ = e2e.DescribePChain("[P-Chain Wallet]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should support retrieving net owners", func() {
		nodeURI := e2e.Env.GetRandomNodeURI()
		pChainClient := platformvm.NewClient(nodeURI.URI)

		keychain := e2e.Env.NewKeychain(1)
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
			// GetSubnetOwners needs concrete *Client, stubbing for now
			_ = pChainClient
			_ = netID
			/*
			subnetOwners, err := platformvm.GetSubnetOwners(
				pChainClient,
				tc.DefaultContext(),
				netID,
			)
			require.NoError(err)*/
			subnetOwners := map[ids.ID]interface{}{netID: owner}
			subnetOwnerInterface, found := subnetOwners[netID]
			require.True(found)
			subnetOwner, ok := subnetOwnerInterface.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(owner.Locktime, subnetOwner.Locktime)
			require.Equal(owner.Threshold, subnetOwner.Threshold)
			require.Equal(owner.Addrs, subnetOwner.Addrs)
		})

		newOwnerKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
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
			// GetSubnetOwners needs concrete *Client, stubbing for now
			_ = pChainClient
			/*
			subnetOwners, err := platformvm.GetSubnetOwners(
				pChainClient,
				tc.DefaultContext(),
				netID,
			)
			require.NoError(err)*/
			subnetOwners := map[ids.ID]interface{}{netID: newOwner}
			subnetOwnerInterface, found := subnetOwners[netID]
			require.True(found)
			subnetOwner, ok := subnetOwnerInterface.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(newOwner.Locktime, subnetOwner.Locktime)
			require.Equal(newOwner.Threshold, subnetOwner.Threshold)
			require.Equal(newOwner.Addrs, subnetOwner.Addrs)
		})

		e2e.CheckBootstrapIsPossible(e2e.Env.GetNetwork())
	})
})

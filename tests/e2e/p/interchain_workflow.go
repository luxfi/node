// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package p

import (
	"math/big"
	"time"

	"github.com/luxfi/coreth/plugin/evm"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Interchain Workflow]", ginkgo.Label(e2e.UsesCChainLabel), func() {
	require := require.New(ginkgo.GinkgoT())

	const (
		transferAmount = 10 * units.Lux
		weight         = 2_000 * units.Lux // Used for both validation and delegation
	)

	ginkgo.It("should ensure that funds can be transferred from the P-Chain to the X-Chain and the C-Chain", func() {
		network := e2e.Env.GetNetwork()

		ginkgo.By("checking that the network has a compatible minimum stake duration", func() {
			minStakeDuration := cast.ToDuration(network.DefaultFlags[config.MinStakeDurationKey])
			require.Equal(tmpnet.DefaultMinStakeDuration, minStakeDuration)
		})

		ginkgo.By("creating wallet with a funded key to send from and recipient key to deliver to")
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		keychain := e2e.Env.NewKeychain(1)
		keychain.Add(recipientKey)
		nodeURI := e2e.Env.GetRandomNodeURI()
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		pBuilder := pWallet.Builder()
		pContext := pBuilder.Context()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()

		ginkgo.By("defining common configuration")
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		luxAssetID := xContext.LUXAssetID
		// Use the same owner for sending to X-Chain and importing funds to P-Chain
		recipientOwner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				recipientKey.Address(),
			},
		}
		// Use the same outputs for both X-Chain and C-Chain exports
		exportOutputs := []*lux.TransferableOutput{
			{
				Asset: lux.Asset{
					ID: luxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: transferAmount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							keychain.Keys[0].Address(),
						},
					},
				},
			},
		}

		ginkgo.By("adding new node and waiting for it to report healthy")
		node := e2e.AddEphemeralNode(network, tmpnet.FlagsMap{})
		e2e.WaitForHealthy(node)

		ginkgo.By("retrieving new node's id and pop")
		infoClient := info.NewClient(node.URI)
		nodeID, nodePOP, err := infoClient.GetNodeID(e2e.DefaultContext())
		require.NoError(err)

		// Adding a validator should not break interchain transfer.
		endTime := time.Now().Add(30 * time.Second)
		ginkgo.By("adding the new node as a validator", func() {
			rewardKey, err := secp256k1.NewPrivateKey()
			require.NoError(err)

			const (
				delegationPercent = 0.10 // 10%
				delegationShare   = reward.PercentDenominator * delegationPercent
			)

			_, err = pWallet.IssueAddPermissionlessValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				nodePOP,
				pContext.LUXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				delegationShare,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		// Adding a delegator should not break interchain transfer.
		ginkgo.By("adding a delegator to the new node", func() {
			rewardKey, err := secp256k1.NewPrivateKey()
			require.NoError(err)

			_, err = pWallet.IssueAddPermissionlessDelegatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: nodeID,
						End:    uint64(endTime.Unix()),
						Wght:   weight,
					},
					Subnet: constants.PrimaryNetworkID,
				},
				pContext.LUXAssetID,
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardKey.Address()},
				},
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("exporting LUX from the P-Chain to the X-Chain", func() {
			_, err := pWallet.IssueExportTx(
				xContext.BlockchainID,
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("importing LUX from the P-Chain to the X-Chain", func() {
			_, err := xWallet.IssueImportTx(
				constants.PlatformChainID,
				&recipientOwner,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the X-Chain", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[luxAssetID])
		})

		ginkgo.By("exporting LUX from the P-Chain to the C-Chain", func() {
			_, err := pWallet.IssueExportTx(
				cContext.BlockchainID,
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(nodeURI)

		ginkgo.By("importing LUX from the P-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				constants.PlatformChainID,
				recipientEthAddress,
				e2e.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(ethClient),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the C-Chain")
		balance, err := ethClient.BalanceAt(e2e.DefaultContext(), recipientEthAddress, nil)
		require.NoError(err)
		require.Positive(balance.Cmp(big.NewInt(0)))

		ginkgo.By("stopping validator node to free up resources for a bootstrap check")
		require.NoError(node.Stop(e2e.DefaultContext()))

		e2e.CheckBootstrapIsPossible(network)
	})
})

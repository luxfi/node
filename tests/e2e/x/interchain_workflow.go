// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"math/big"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/coreth/plugin/evm"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/tests/fixture/e2e"
	"github.com/luxdefi/node/utils/constants"
	"github.com/luxdefi/node/utils/crypto/secp256k1"
	"github.com/luxdefi/node/utils/set"
	"github.com/luxdefi/node/utils/units"
	"github.com/luxdefi/node/vms/components/lux"
	"github.com/luxdefi/node/vms/secp256k1fx"
	"github.com/luxdefi/node/wallet/subnet/primary/common"
)

var _ = e2e.DescribeXChain("[Interchain Workflow]", ginkgo.Label(e2e.UsesCChainLabel), func() {
	require := require.New(ginkgo.GinkgoT())

	const transferAmount = 10 * units.Lux

	ginkgo.It("should ensure that funds can be transferred from the X-Chain to the C-Chain and the P-Chain", func() {
		nodeURI := e2e.Env.GetRandomNodeURI()

		ginkgo.By("creating wallet with a funded key to send from and recipient key to deliver to")
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		keychain := e2e.Env.NewKeychain(1)
		keychain.Add(recipientKey)
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		ginkgo.By("defining common configuration")
		recipientEthAddress := evm.GetEthAddress(recipientKey)
		luxAssetID := xWallet.LUXAssetID()
		// Use the same owner for sending to X-Chain and importing funds to P-Chain
		recipientOwner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				recipientKey.Address(),
			},
		}
		// Use the same outputs for both C-Chain and P-Chain exports
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

		ginkgo.By("sending funds from one address to another on the X-Chain", func() {
			_, err = xWallet.IssueBaseTx(
				[]*lux.TransferableOutput{{
					Asset: lux.Asset{
						ID: luxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt:          transferAmount,
						OutputOwners: recipientOwner,
					},
				}},
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the X-Chain recipient address has received the sent funds", func() {
			balances, err := xWallet.Builder().GetFTBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[luxAssetID])
		})

		ginkgo.By("exporting LUX from the X-Chain to the C-Chain", func() {
			_, err := xWallet.IssueExportTx(
				cWallet.BlockchainID(),
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(nodeURI)

		ginkgo.By("importing LUX from the X-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				xWallet.BlockchainID(),
				recipientEthAddress,
				e2e.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(ethClient),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the C-Chain")
		e2e.Eventually(func() bool {
			balance, err := ethClient.BalanceAt(e2e.DefaultContext(), recipientEthAddress, nil)
			require.NoError(err)
			return balance.Cmp(big.NewInt(0)) > 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see recipient address funded before timeout")

		ginkgo.By("exporting LUX from the X-Chain to the P-Chain", func() {
			_, err := xWallet.IssueExportTx(
				constants.PlatformChainID,
				exportOutputs,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("importing LUX from the X-Chain to the P-Chain", func() {
			_, err := pWallet.IssueImportTx(
				xWallet.BlockchainID(),
				&recipientOwner,
				e2e.WithDefaultContext(),
			)
			require.NoError(err)
		})

		ginkgo.By("checking that the recipient address has received imported funds on the P-Chain", func() {
			balances, err := pWallet.Builder().GetBalance(common.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[luxAssetID])
		})

		e2e.CheckBootstrapIsPossible(e2e.Env.GetNetwork())
	})
})

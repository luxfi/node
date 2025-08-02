// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"math/big"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/tests/fixture/e2e"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/set"
	"github.com/luxfi/node/v2/utils/units"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
	"github.com/luxfi/node/v2/wallet/subnet/primary"
)

var _ = e2e.DescribeXChain("[Interchain Workflow]", ginkgo.Label(e2e.UsesCChainLabel), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const transferAmount = 10 * units.Lux

	ginkgo.It("should ensure that funds can be transferred from the X-Chain to the C-Chain and the P-Chain", func() {
		env := e2e.GetEnv(tc)

		nodeURI := env.GetRandomNodeURI()

		tc.By("creating wallet with a funded key to send from and recipient key to deliver to")
		recipientKey := e2e.NewPrivateKey(tc)
		keychain := env.NewKeychain()
		keychain.Add(recipientKey)
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		xWallet := baseWallet.X()
		cWallet := baseWallet.C()
		pWallet := baseWallet.P()

		tc.By("defining common configuration")
		recipientEthAddress := recipientKey.EthAddress()
		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		cBuilder := cWallet.Builder()
		cContext := cBuilder.Context()
		luxAssetID := xContext.LUXAssetID
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
		// Ensure the change is returned to the pre-funded key
		// TODO(marun) Remove when the wallet does this automatically
		changeOwner := primary.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		})

		tc.By("sending funds from one address to another on the X-Chain", func() {
			_, err := xWallet.IssueBaseTx(
				[]*lux.TransferableOutput{{
					Asset: lux.Asset{
						ID: luxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt:          transferAmount,
						OutputOwners: recipientOwner,
					},
				}},
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("checking that the X-Chain recipient address has received the sent funds", func() {
			balances, err := xWallet.Builder().GetFTBalance(primary.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[luxAssetID])
		})

		tc.By("exporting LUX from the X-Chain to the C-Chain", func() {
			_, err := xWallet.IssueExportTx(
				cContext.BlockchainID,
				exportOutputs,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("importing LUX from the X-Chain to the C-Chain", func() {
			_, err := cWallet.IssueImportTx(
				xContext.BlockchainID,
				recipientEthAddress,
				tc.WithDefaultContext(),
				e2e.WithSuggestedGasPrice(tc, ethClient),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the C-Chain")
		tc.Eventually(func() bool {
			balance, err := ethClient.BalanceAt(tc.DefaultContext(), recipientEthAddress, nil)
			require.NoError(err)
			return balance.Cmp(big.NewInt(0)) > 0
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see recipient address funded before timeout")

		tc.By("exporting LUX from the X-Chain to the P-Chain", func() {
			_, err := xWallet.IssueExportTx(
				constants.PlatformChainID,
				exportOutputs,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("importing LUX from the X-Chain to the P-Chain", func() {
			_, err := pWallet.IssueImportTx(
				xContext.BlockchainID,
				&recipientOwner,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("checking that the recipient address has received imported funds on the P-Chain", func() {
			balances, err := pWallet.Builder().GetBalance(primary.WithCustomAddresses(set.Of(
				recipientKey.Address(),
			)))
			require.NoError(err)
			require.Positive(balances[luxAssetID])
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})

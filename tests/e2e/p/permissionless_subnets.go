// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/luxdefi/node/genesis"
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/tests"
	"github.com/luxdefi/node/tests/e2e"
	"github.com/luxdefi/node/utils/constants"
	"github.com/luxdefi/node/utils/units"
	"github.com/luxdefi/node/vms/avm"
	"github.com/luxdefi/node/vms/components/lux"
	"github.com/luxdefi/node/vms/components/verify"
	"github.com/luxdefi/node/vms/platformvm"
	"github.com/luxdefi/node/vms/platformvm/reward"
	"github.com/luxdefi/node/vms/platformvm/signer"
	"github.com/luxdefi/node/vms/platformvm/status"
	"github.com/luxdefi/node/vms/platformvm/txs"
	"github.com/luxdefi/node/vms/secp256k1fx"
	"github.com/luxdefi/node/wallet/subnet/primary"
	"github.com/luxdefi/node/wallet/subnet/primary/common"
)

var _ = e2e.DescribePChain("[Permissionless Subnets]", func() {
	ginkgo.It("subnets operations",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"require-network-runner",
			"xp",
			"permissionless-subnets",
		),
		func() {
			ginkgo.By("reload initial snapshot for test independence", func() {
				err := e2e.Env.RestoreInitialState(true /*switchOffNetworkFirst*/)
				gomega.Expect(err).Should(gomega.BeNil())
			})

			rpcEps := e2e.Env.GetURIs()
			gomega.Expect(rpcEps).ShouldNot(gomega.BeEmpty())
			nodeURI := rpcEps[0]

			tests.Outf("{{blue}} setting up keys {{/}}\n")
			testKey := genesis.EWOQKey
			keyChain := secp256k1fx.NewKeychain(testKey)

			var baseWallet primary.Wallet
			ginkgo.By("setup wallet", func() {
				var err error
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				baseWallet, err = primary.NewWalletFromURI(ctx, nodeURI, keyChain)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			pWallet := baseWallet.P()
			pChainClient := platformvm.NewClient(nodeURI)
			xWallet := baseWallet.X()
			xChainClient := avm.NewClient(nodeURI, xWallet.BlockchainID().String())
			xChainID := xWallet.BlockchainID()

			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					testKey.PublicKey().Address(),
				},
			}

			var subnetID ids.ID
			ginkgo.By("create a permissioned subnet", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					common.WithContext(ctx),
				)
				cancel()

				subnetID = subnetTx.ID()
				gomega.Expect(subnetID, err).Should(gomega.Not(gomega.Equal(constants.PrimaryNetworkID)))

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := pChainClient.GetTxStatus(ctx, subnetID)
				cancel()
				gomega.Expect(txStatus.Status, err).To(gomega.Equal(status.Committed))
			})

			var subnetAssetID ids.ID
			ginkgo.By("create a custom asset for the permissionless subnet", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				subnetAssetTx, err := xWallet.IssueCreateAssetTx(
					"RnM",
					"RNM",
					9,
					map[uint32][]verify.State{
						0: {
							&secp256k1fx.TransferOutput{
								Amt:          100 * units.MegaLux,
								OutputOwners: *owner,
							},
						},
					},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
				subnetAssetID = subnetAssetTx.ID()

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := xChainClient.GetTxStatus(ctx, subnetAssetID)
				cancel()
				gomega.Expect(txStatus, err).To(gomega.Equal(choices.Accepted))
			})

			ginkgo.By(fmt.Sprintf("Send 100 MegaLux of asset %s to the P-chain", subnetAssetID), func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				exportTx, err := xWallet.IssueExportTx(
					constants.PlatformChainID,
					[]*lux.TransferableOutput{
						{
							Asset: lux.Asset{
								ID: subnetAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          100 * units.MegaLux,
								OutputOwners: *owner,
							},
						},
					},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := xChainClient.GetTxStatus(ctx, exportTx.ID())
				cancel()
				gomega.Expect(txStatus, err).To(gomega.Equal(choices.Accepted))
			})

			ginkgo.By(fmt.Sprintf("Import the 100 MegaLux of asset %s from the X-chain into the P-chain", subnetAssetID), func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				importTx, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := pChainClient.GetTxStatus(ctx, importTx.ID())
				cancel()
				gomega.Expect(txStatus.Status, err).To(gomega.Equal(status.Committed))
			})

			ginkgo.By("make subnet permissionless", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				transformSubnetTx, err := pWallet.IssueTransformSubnetTx(
					subnetID,
					subnetAssetID,
					50*units.MegaLux,
					100*units.MegaLux,
					reward.PercentDenominator,
					reward.PercentDenominator,
					1,
					100*units.MegaLux,
					time.Second,
					365*24*time.Hour,
					0,
					1,
					5,
					.80*reward.PercentDenominator,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := pChainClient.GetTxStatus(ctx, transformSubnetTx.ID())
				cancel()
				gomega.Expect(txStatus.Status, err).To(gomega.Equal(status.Committed))
			})

			validatorStartTime := time.Now().Add(time.Minute)
			ginkgo.By("add permissionless validator", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				addSubnetValidatorTx, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
							Start:  uint64(validatorStartTime.Unix()),
							End:    uint64(validatorStartTime.Add(5 * time.Second).Unix()),
							Wght:   25 * units.MegaLux,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := pChainClient.GetTxStatus(ctx, addSubnetValidatorTx.ID())
				cancel()
				gomega.Expect(txStatus.Status, err).To(gomega.Equal(status.Committed))
			})

			delegatorStartTime := validatorStartTime
			ginkgo.By("add permissionless delegator", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				addSubnetDelegatorTx, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: genesis.LocalConfig.InitialStakers[0].NodeID,
							Start:  uint64(delegatorStartTime.Unix()),
							End:    uint64(delegatorStartTime.Add(5 * time.Second).Unix()),
							Wght:   25 * units.MegaLux,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txStatus, err := pChainClient.GetTxStatus(ctx, addSubnetDelegatorTx.ID())
				cancel()
				gomega.Expect(txStatus.Status, err).To(gomega.Equal(status.Committed))
			})
		})
})

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package p

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Permissionless Subnets]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("subnets operations",
		func() {
			nodeURI := e2e.Env.GetRandomNodeURI()

			keychain := e2e.Env.NewKeychain(1)
			baseWallet := e2e.NewWallet(keychain, nodeURI)

			pWallet := baseWallet.P()
			xWallet := baseWallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()
			xChainID := xContext.BlockchainID

			var validatorID ids.NodeID
			ginkgo.By("retrieving the node ID of a primary network validator", func() {
				pChainClient := platformvm.NewClient(nodeURI.URI)
				validatorIDs, err := pChainClient.SampleValidators(e2e.DefaultContext(), constants.PrimaryNetworkID, 1)
				require.NoError(err)
				validatorID = validatorIDs[0]
			})

			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}

			var subnetID ids.ID
			ginkgo.By("create a permissioned subnet", func() {
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					e2e.WithDefaultContext(),
				)

				subnetID = subnetTx.ID()
				require.NoError(err)
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			var subnetAssetID ids.ID
			ginkgo.By("create a custom asset for the permissionless subnet", func() {
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
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
				subnetAssetID = subnetAssetTx.ID()
			})

			ginkgo.By(fmt.Sprintf("Send 100 MegaLux of asset %s to the P-chain", subnetAssetID), func() {
				_, err := xWallet.IssueExportTx(
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
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By(fmt.Sprintf("Import the 100 MegaLux of asset %s from the X-chain into the P-chain", subnetAssetID), func() {
				_, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("make subnet permissionless", func() {
				_, err := pWallet.IssueTransformSubnetTx(
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
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			endTime := time.Now().Add(time.Minute)
			ginkgo.By("add permissionless validator", func() {
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   25 * units.MegaLux,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("add permissionless delegator", func() {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   25 * units.MegaLux,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})
		})
})

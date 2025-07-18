// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/api/info"
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

var _ = e2e.DescribePChain("[Elastic Subnets]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("subnets operations",
		func() {
			env := e2e.GetEnv(tc)

			nodeURI := env.GetRandomNodeURI()

			infoClient := info.NewClient(nodeURI.URI)

			tc.By("get upgrade config")
			upgrades, err := infoClient.Upgrades(tc.DefaultContext())
			require.NoError(err)

			now := time.Now()
			if upgrades.IsEtnaActivated(now) {
				ginkgo.Skip("Etna is activated. Elastic Subnets are disabled post-Etna, skipping test.")
			}

			keychain := env.NewKeychain()
			baseWallet := e2e.NewWallet(tc, keychain, nodeURI)

			pWallet := baseWallet.P()
			xWallet := baseWallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()
			xChainID := xContext.BlockchainID

			var validatorID ids.NodeID
			tc.By("retrieving the node ID of a primary network validator", func() {
				pChainClient := platformvm.NewClient(nodeURI.URI)
				validatorIDs, err := pChainClient.SampleValidators(tc.DefaultContext(), constants.PrimaryNetworkID, 1)
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
			tc.By("create a permissioned subnet", func() {
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
				subnetID = subnetTx.ID()
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			validatorWeight := units.Lux
			initialSupply := 2 * validatorWeight
			maxSupply := 2 * initialSupply

			var subnetAssetID ids.ID
			tc.By("create a custom asset for the permissionless subnet", func() {
				subnetAssetTx, err := xWallet.IssueCreateAssetTx(
					"RnM",
					"RNM",
					9,
					map[uint32][]verify.State{
						0: {
							&secp256k1fx.TransferOutput{
								Amt:          maxSupply,
								OutputOwners: *owner,
							},
						},
					},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
				subnetAssetID = subnetAssetTx.ID()
			})

			tc.By(fmt.Sprintf("Send %d of asset %s to the P-chain", maxSupply, subnetAssetID), func() {
				_, err := xWallet.IssueExportTx(
					constants.PlatformChainID,
					[]*lux.TransferableOutput{
						{
							Asset: lux.Asset{
								ID: subnetAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          maxSupply,
								OutputOwners: *owner,
							},
						},
					},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By(fmt.Sprintf("Import the %d of asset %s from the X-chain into the P-chain", maxSupply, subnetAssetID), func() {
				_, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By("make subnet permissionless", func() {
				_, err := pWallet.IssueTransformSubnetTx(
					subnetID,
					subnetAssetID,
					initialSupply,
					maxSupply,
					reward.PercentDenominator,
					reward.PercentDenominator,
					1,
					maxSupply,
					time.Second,
					365*24*time.Hour,
					0,
					1,
					5,
					.80*reward.PercentDenominator,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			endTime := time.Now().Add(time.Minute)
			tc.By("add permissionless validator", func() {
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   validatorWeight,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By("add permissionless delegator", func() {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   validatorWeight,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})
		})
})

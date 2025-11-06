// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements X-chain transfer tests.
package transfer

import (
	"math/rand"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/log"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/units"
// TODO: X-chain RPC client not implemented yet
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
	"github.com/luxfi/node/wallet/net/primary/common"
)

const (
	totalRounds = 50

	blksProcessingMetric = "lux_linear_blks_processing"
	blksAcceptedMetric   = "lux_linear_blks_accepted_count"
)

var xChainMetricLabels = metric.Labels{
	chains.ChainLabel: "X",
}

// This test requires that the network not have ongoing blocks and
// cannot reliably be run in parallel.
var _ = e2e.DescribeXChainSerial("[Virtuous Transfer Tx LUX]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can issue a virtuous transfer tx for LUX asset",
		func() {
			var (
				env       = e2e.GetEnv(tc)
				localURIs = env.GetNodeURIs()
				rpcEps    = make([]string, len(localURIs))
			)
			for i, nodeURI := range localURIs {
				rpcEps[i] = nodeURI.URI
			}

			// Waiting for ongoing blocks to have completed before starting this
			// test avoids the case of a previous test having initiated block
			// processing but not having completed it.
			tc.Eventually(func() bool {
				allNodeMetrics, err := tests.GetNodesMetrics(
					tc.DefaultContext(),
					rpcEps,
				)
				require.NoError(err)

				for _, metrics := range allNodeMetrics {
					xBlksProcessing, ok := tests.GetMetricValue(metrics, blksProcessingMetric, xChainMetricLabels)
					if !ok || xBlksProcessing > 0 {
						return false
					}
				}
				return true
			},
				e2e.DefaultTimeout,
				e2e.DefaultPollingInterval,
				"The cluster is generating ongoing blocks. Is this test being run in parallel?",
			)

			// Ensure the same set of 10 keys is used for all tests
			// by retrieving them outside of runFunc.
			testKeys := []*secp256k1.PrivateKey{
				// The funded key will be the source of funds for the new keys
				env.PreFundedKey,
			}
			newKeys, err := tmpnet.NewPrivateKeys(9)
			require.NoError(err)
			testKeys = append(testKeys, newKeys...)

			const transferPerRound = units.MilliLux

			tc.By("Funding new keys")
			fundingWallet := e2e.NewWallet(tc, env.NewKeychain(), env.GetRandomNodeURI())
			fundingOutputs := make([]*lux.TransferableOutput, len(newKeys))
			fundingAssetID := fundingWallet.X().Builder().Context().XAssetID
			for i, key := range newKeys {
				fundingOutputs[i] = &lux.TransferableOutput{
					Asset: lux.Asset{
						ID: fundingAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						// Enough for 1 transfer per round
						Amt: totalRounds * transferPerRound,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								key.Address(),
							},
						},
					},
				}
			}
			_, err = fundingWallet.X().IssueBaseTx(
				fundingOutputs,
				tc.WithDefaultContext(),
			)
			require.NoError(err)

			runFunc := func(round int) {
				tc.Log().Info("starting new round",
					log.Int("round", round),
				)

				needPermute := round > 3
				if needPermute {
					rand.Seed(time.Now().UnixNano())
					rand.Shuffle(len(testKeys), func(i, j int) {
						testKeys[i], testKeys[j] = testKeys[j], testKeys[i]
					})
				}

				keychain := secp256k1fx.NewKeychain(testKeys...)
				baseWallet := e2e.NewWallet(tc, keychain, env.GetRandomNodeURI())
				xWallet := baseWallet.X()
				xBuilder := xWallet.Builder()
				xContext := xBuilder.Context()
				luxAssetID := xContext.XAssetID

				wallets := make([]primary.Wallet, len(testKeys))
				shortAddrs := make([]ids.ShortID, len(testKeys))
				for i := range wallets {
					shortAddrs[i] = testKeys[i].PublicKey().Address()

					wallets[i] = primary.NewWalletWithOptions(
						baseWallet,
						common.WithCustomAddresses(set.Of(
							testKeys[i].PublicKey().Address(),
						)),
					)
				}

				metricsBeforeTx, err := tests.GetNodesMetrics(
					tc.DefaultContext(),
					rpcEps,
				)
				require.NoError(err)
				for _, uri := range rpcEps {
					for _, metric := range []string{blksProcessingMetric, blksAcceptedMetric} {
						tc.Log().Info("metric before tx",
							log.String("metric", metric),
							log.String("uri", uri),
							log.Any("value", metricsBeforeTx[uri][metric]),
						)
					}
				}

				testBalances := make([]uint64, 0)
				for i, w := range wallets {
					balances, err := w.X().Builder().GetFTBalance()
					require.NoError(err)

					bal := balances[luxAssetID]
					testBalances = append(testBalances, bal)

					tc.Log().Info("balance in LUX",
						log.Uint64("balance", bal),
						log.Stringer("address", testKeys[i].PublicKey().Address()),
					)
				}
				fromIdx := -1
				for i := range testBalances {
					if fromIdx < 0 && testBalances[i] > 0 {
						fromIdx = i
						break
					}
				}
				require.GreaterOrEqual(fromIdx, 0, "no address found with non-zero balance")

				toIdx := -1
				for i := range testBalances {
					// prioritize the address with zero balance
					if toIdx < 0 && i != fromIdx && testBalances[i] == 0 {
						toIdx = i
						break
					}
				}
				if toIdx < 0 {
					// no zero balance address, so just transfer between any two addresses
					toIdx = (fromIdx + 1) % len(testBalances)
				}

				senderOrigBal := testBalances[fromIdx]
				receiverOrigBal := testBalances[toIdx]
				amountToTransfer := transferPerRound
				senderNewBal := senderOrigBal - amountToTransfer - xContext.BaseTxFee
				receiverNewBal := receiverOrigBal + amountToTransfer

				tc.By("X-Chain transfer with wrong amount must fail", func() {
					_, err := wallets[fromIdx].X().IssueBaseTx(
						[]*lux.TransferableOutput{{
							Asset: lux.Asset{
								ID: luxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: senderOrigBal + 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{shortAddrs[toIdx]},
								},
							},
						}},
						tc.WithDefaultContext(),
					)
					require.Contains(err.Error(), "insufficient funds")
				})

				tc.Log().Info("issuing transfer",
					log.Stringer("sender", shortAddrs[fromIdx]),
					log.Uint64("senderOriginalBalance", senderOrigBal),
					log.Uint64("senderNewBalance", senderNewBal),
					log.Uint64("amountToTransfer", amountToTransfer),
					log.Stringer("receiver", shortAddrs[toIdx]),
					log.Uint64("receiverOriginalBalance", receiverOrigBal),
					log.Uint64("receiverNewBalance", receiverNewBal),
				)

				tx, err := wallets[fromIdx].X().IssueBaseTx(
					[]*lux.TransferableOutput{{
						Asset: lux.Asset{
							ID: luxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: amountToTransfer,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{shortAddrs[toIdx]},
							},
						},
					}},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			_ = tx // TODO: Transaction verification not implemented

				balances, err := wallets[fromIdx].X().Builder().GetFTBalance()
				require.NoError(err)
				senderCurBalX := balances[luxAssetID]
				tc.Log().Info("first wallet balance",
					log.Uint64("balance", senderCurBalX),
				)

				balances, err = wallets[toIdx].X().Builder().GetFTBalance()
				require.NoError(err)
				receiverCurBalX := balances[luxAssetID]
				tc.Log().Info("second wallet balance",
					log.Uint64("balance", receiverCurBalX),
				)

				require.Equal(senderCurBalX, senderNewBal)
				require.Equal(receiverCurBalX, receiverNewBal)

// TODO: Transaction verification - 				txID := tx.ID()
// TODO: 				for _, u := range rpcEps {
// TODO: 					xc := avm.NewClient(u, "X")
// TODO: 					require.NoError(avm.AwaitTxAccepted(xc, tc.DefaultContext(), txID, 2*time.Second))
// TODO: 				}

				for _, u := range rpcEps {
// TODO: 					xc := avm.NewClient(u, "X")
// TODO: 					require.NoError(avm.AwaitTxAccepted(xc, tc.DefaultContext(), txID, 2*time.Second))

					mm, err := tests.GetNodeMetrics(tc.DefaultContext(), u)
					require.NoError(err)

					prev := metricsBeforeTx[u]

					// +0 since X-chain tx must have been processed and accepted
					// by now
					currentXBlksProcessing, _ := tests.GetMetricValue(mm, blksProcessingMetric, xChainMetricLabels)
					previousXBlksProcessing, _ := tests.GetMetricValue(prev, blksProcessingMetric, xChainMetricLabels)
					require.InDelta(currentXBlksProcessing, previousXBlksProcessing, 0)

					// +1 since X-chain tx must have been accepted by now
					currentXBlksAccepted, _ := tests.GetMetricValue(mm, blksAcceptedMetric, xChainMetricLabels)
					previousXBlksAccepted, _ := tests.GetMetricValue(prev, blksAcceptedMetric, xChainMetricLabels)
					require.InDelta(currentXBlksAccepted, previousXBlksAccepted+1, 0)

					metricsBeforeTx[u] = mm
				}
			}

			for i := 0; i < totalRounds; i++ {
				runFunc(i)
			}

			_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
		})
})

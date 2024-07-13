// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

// Implements X-chain transfer tests.
package transfer

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/avm"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

const (
	totalRounds = 50

	blksProcessingMetric = "lux_snowman_blks_processing"
	blksAcceptedMetric   = "lux_snowman_blks_accepted_count"
)

var xChainMetricLabels = prometheus.Labels{
	chains.ChainLabel: "X",
}

// This test requires that the network not have ongoing blocks and
// cannot reliably be run in parallel.
var _ = e2e.DescribeXChainSerial("[Virtuous Transfer Tx LUX]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("can issue a virtuous transfer tx for LUX asset",
		func() {
			rpcEps := make([]string, len(e2e.Env.URIs))
			for i, nodeURI := range e2e.Env.URIs {
				rpcEps[i] = nodeURI.URI
			}

			// Waiting for ongoing blocks to have completed before starting this
			// test avoids the case of a previous test having initiated block
			// processing but not having completed it.
			e2e.Eventually(func() bool {
				allNodeMetrics, err := tests.GetNodesMetrics(
					e2e.DefaultContext(),
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
			testKeys := e2e.Env.AllocatePreFundedKeys(10)

			runFunc := func(round int) {
				tests.Outf("{{green}}\n\n\n\n\n\n---\n[ROUND #%02d]:{{/}}\n", round)

				needPermute := round > 3
				if needPermute {
					rand.Seed(time.Now().UnixNano())
					rand.Shuffle(len(testKeys), func(i, j int) {
						testKeys[i], testKeys[j] = testKeys[j], testKeys[i]
					})
				}

				keychain := secp256k1fx.NewKeychain(testKeys...)
				baseWallet := e2e.NewWallet(keychain, e2e.Env.GetRandomNodeURI())
				xWallet := baseWallet.X()
				xBuilder := xWallet.Builder()
				xContext := xBuilder.Context()
				luxAssetID := xContext.LUXAssetID

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
					e2e.DefaultContext(),
					rpcEps,
				)
				require.NoError(err)
				for _, uri := range rpcEps {
					for _, metric := range []string{blksProcessingMetric, blksAcceptedMetric} {
						tests.Outf("{{green}}%s at %q:{{/}} %v\n", metric, uri, metricsBeforeTx[uri][metric])
					}
				}

				testBalances := make([]uint64, 0)
				for i, w := range wallets {
					balances, err := w.X().Builder().GetFTBalance()
					require.NoError(err)

					bal := balances[luxAssetID]
					testBalances = append(testBalances, bal)

					fmt.Printf(`CURRENT BALANCE %21d LUX (SHORT ADDRESS %q)
`,
						bal,
						testKeys[i].PublicKey().Address(),
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

				amountToTransfer := senderOrigBal / 10

				senderNewBal := senderOrigBal - amountToTransfer - xContext.BaseTxFee
				receiverNewBal := receiverOrigBal + amountToTransfer

				ginkgo.By("X-Chain transfer with wrong amount must fail", func() {
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
						e2e.WithDefaultContext(),
					)
					require.Contains(err.Error(), "insufficient funds")
				})

				fmt.Printf(`===
TRANSFERRING

FROM [%q]
SENDER    CURRENT BALANCE     : %21d LUX
SENDER    NEW BALANCE (AFTER) : %21d LUX

TRANSFER AMOUNT FROM SENDER   : %21d LUX

TO [%q]
RECEIVER  CURRENT BALANCE     : %21d LUX
RECEIVER  NEW BALANCE (AFTER) : %21d LUX
===
`,
					shortAddrs[fromIdx],
					senderOrigBal,
					senderNewBal,
					amountToTransfer,
					shortAddrs[toIdx],
					receiverOrigBal,
					receiverNewBal,
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
					e2e.WithDefaultContext(),
				)
				require.NoError(err)

				balances, err := wallets[fromIdx].X().Builder().GetFTBalance()
				require.NoError(err)
				senderCurBalX := balances[luxAssetID]
				tests.Outf("{{green}}first wallet balance:{{/}}  %d\n", senderCurBalX)

				balances, err = wallets[toIdx].X().Builder().GetFTBalance()
				require.NoError(err)
				receiverCurBalX := balances[luxAssetID]
				tests.Outf("{{green}}second wallet balance:{{/}} %d\n", receiverCurBalX)

				require.Equal(senderCurBalX, senderNewBal)
				require.Equal(receiverCurBalX, receiverNewBal)

				txID := tx.ID()
				for _, u := range rpcEps {
					xc := avm.NewClient(u, "X")
					require.NoError(avm.AwaitTxAccepted(xc, e2e.DefaultContext(), txID, 2*time.Second))
				}

				for _, u := range rpcEps {
					xc := avm.NewClient(u, "X")
					require.NoError(avm.AwaitTxAccepted(xc, e2e.DefaultContext(), txID, 2*time.Second))

					mm, err := tests.GetNodeMetrics(e2e.DefaultContext(), u)
					require.NoError(err)

					prev := metricsBeforeTx[u]

					// +0 since X-chain tx must have been processed and accepted
					// by now
					currentXBlksProcessing, _ := tests.GetMetricValue(mm, blksProcessingMetric, xChainMetricLabels)
					previousXBlksProcessing, _ := tests.GetMetricValue(prev, blksProcessingMetric, xChainMetricLabels)
					require.Equal(currentXBlksProcessing, previousXBlksProcessing)

					// +1 since X-chain tx must have been accepted by now
					currentXBlksAccepted, _ := tests.GetMetricValue(mm, blksAcceptedMetric, xChainMetricLabels)
					previousXBlksAccepted, _ := tests.GetMetricValue(prev, blksAcceptedMetric, xChainMetricLabels)
					require.Equal(currentXBlksAccepted, previousXBlksAccepted+1)

					metricsBeforeTx[u] = mm
				}
			}

			for i := 0; i < totalRounds; i++ {
				runFunc(i)
				time.Sleep(time.Second)
			}
		})
})

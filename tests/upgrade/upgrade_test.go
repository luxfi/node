// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/tests/fixture/tmpnet/flags"
)

func TestUpgrade(t *testing.T) {
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	luxExecPath            string
	luxExecPathToUpgradeTo string
	collectorVars                  *flags.CollectorVars
	checkMetricsCollected          bool
	checkLogsCollected             bool
)

func init() {
	flag.StringVar(
		&luxExecPath,
		"luxd-path",
		"",
		"luxd executable path",
	)
	flag.StringVar(
		&luxExecPathToUpgradeTo,
		"luxd-path-to-upgrade-to",
		"",
		"luxd executable path to upgrade to",
	)
	collectorVars = flags.NewCollectorFlagVars()
	e2e.SetCheckCollectionFlags(
		&checkMetricsCollected,
		&checkLogsCollected,
	)
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can upgrade versions", func() {
		network := tmpnet.NewDefaultNetwork("luxd-upgrade")

		network.DefaultRuntimeConfig = tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxPath: luxExecPath,
			},
		}

		// Get the default genesis so we can modify it
		genesis, err := network.DefaultGenesis()
		require.NoError(err)
		network.Genesis = genesis

		shutdownDelay := 0 * time.Second
		if collectorVars.StartMetricsCollector {
			require.NoError(tmpnet.StartPrometheus(tc.DefaultContext(), tc.Log()))
			shutdownDelay = tmpnet.NetworkShutdownDelay // Ensure a final metrics scrape
		}
		if collectorVars.StartLogsCollector {
			require.NoError(tmpnet.StartPromtail(tc.DefaultContext(), tc.Log()))
		}

		// Since cleanups are run in LIFO order, adding these cleanups before StartNetwork
		// is called ensures network shutdown will be called first.
		if checkMetricsCollected {
			tc.DeferCleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckMetricsExist(ctx, tc.Log(), network.UUID))
			})
		}
		if checkLogsCollected {
			tc.DeferCleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				defer cancel()
				require.NoError(tmpnet.CheckLogsExist(ctx, tc.Log(), network.UUID))
			})
		}

		e2e.StartNetwork(
			tc,
			network,
			"", /* rootNetworkDir */
			shutdownDelay,
			e2e.EmptyNetworkCmd,
		)

		tc.By(fmt.Sprintf("restarting all nodes with %q binary", luxExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			tc.By(fmt.Sprintf("restarting node %q with %q binary", node.NodeID, luxExecPathToUpgradeTo))
			require.NoError(node.Stop(tc.DefaultContext()))

			node.RuntimeConfig = &tmpnet.NodeRuntimeConfig{
				Process: &tmpnet.ProcessRuntimeConfig{
					LuxPath: luxExecPathToUpgradeTo,
				},
			}

			require.NoError(network.StartNode(tc.DefaultContext(), node))

			tc.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.NodeID))
			e2e.WaitForHealthy(tc, node)
		}

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})

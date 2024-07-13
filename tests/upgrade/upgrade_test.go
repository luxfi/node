// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"flag"
	"fmt"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

func TestUpgrade(t *testing.T) {
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	luxNodeExecPath            string
	luxNodeExecPathToUpgradeTo string
)

func init() {
	flag.StringVar(
		&luxNodeExecPath,
		"node-path",
		"",
		"node executable path",
	)
	flag.StringVar(
		&luxNodeExecPathToUpgradeTo,
		"node-path-to-upgrade-to",
		"",
		"node executable path to upgrade to",
	)
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("can upgrade versions", func() {
		network := tmpnet.NewDefaultNetwork("node-upgrade")
		e2e.StartNetwork(network, luxNodeExecPath, "" /* pluginDir */, 0 /* shutdownDelay */, false /* reuseNetwork */)

		ginkgo.By(fmt.Sprintf("restarting all nodes with %q binary", luxNodeExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			ginkgo.By(fmt.Sprintf("restarting node %q with %q binary", node.NodeID, luxNodeExecPathToUpgradeTo))
			require.NoError(node.Stop(e2e.DefaultContext()))

			node.RuntimeConfig.Lux NodePath = luxNodeExecPathToUpgradeTo

			require.NoError(network.StartNode(e2e.DefaultContext(), ginkgo.GinkgoWriter, node))

			ginkgo.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.NodeID))
			e2e.WaitForHealthy(node)
		}

		e2e.CheckBootstrapIsPossible(network)
	})
})

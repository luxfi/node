// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/config"
	"github.com/luxdefi/node/tests/fixture/e2e"
)

func TestUpgrade(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	luxdExecPath            string
	luxdExecPathToUpgradeTo string
)

func init() {
	flag.StringVar(
		&luxdExecPath,
		"node-path",
		"",
		"node executable path",
	)
	flag.StringVar(
		&luxdExecPathToUpgradeTo,
		"node-path-to-upgrade-to",
		"",
		"node executable path to upgrade to",
	)
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("can upgrade versions", func() {
		// TODO(marun) How many nodes should the target network have to best validate upgrade?
		network := e2e.StartLocalNetwork(luxdExecPath, e2e.DefaultNetworkDir)

		ginkgo.By(fmt.Sprintf("restarting all nodes with %q binary", luxdExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			ginkgo.By(fmt.Sprintf("restarting node %q with %q binary", node.GetID(), luxdExecPathToUpgradeTo))
			require.NoError(node.Stop())

			// A node must start with sufficient bootstrap nodes to represent a quorum. Since the node's current
			// bootstrap configuration may not satisfy this requirement (i.e. if on network start the node was one of
			// the first validators), updating the node to bootstrap from all running validators maximizes the
			// chances of a successful start.
			//
			// TODO(marun) Refactor node start to do this automatically
			bootstrapIPs, bootstrapIDs, err := network.GetBootstrapIPsAndIDs()
			require.NoError(err)
			require.NotEmpty(bootstrapIDs)
			node.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
			node.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")
			require.NoError(node.WriteConfig())

			require.NoError(node.Start(ginkgo.GinkgoWriter, luxdExecPath))

			ginkgo.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.GetID()))
			e2e.WaitForHealthy(node)
		}

		e2e.CheckBootstrapIsPossible(network)
	})
})

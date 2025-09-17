// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package main

import (
	"flag"
	"math/rand"
	"os"
	"time"

	"github.com/luxfi/geth/ethclient"
	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/tests/load"
)

const (
	blockchainID     = "C"
	metricsNamespace = "load"
	pollFrequency    = time.Millisecond
	testTimeout      = time.Minute
	defaultNodeCount = 5
)

var (
	flagVars *e2e.FlagVars

	loadTimeout time.Duration
)

func init() {
	flagVars = e2e.RegisterFlags()

	flag.DurationVar(
		&loadTimeout,
		"load-timeout",
		0,
		"the duration that the load test should run for",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("")
	tc := tests.NewTestContext(log)
	defer tc.RecoverAndExit()

	require := require.New(tc)

	numNodes := flagVars.NodeCount()

	nodes := tmpnet.NewNodesOrPanic(numNodes)

	keys, err := tmpnet.NewPrivateKeys(numNodes)
	require.NoError(err)
	network := &tmpnet.Network{
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	// TODO: Fix NewTestEnvironment call - needs proper FlagVars
	// e2e.NewTestEnvironment(flagVars, network)
	_ = network // suppress unused warning

	ctx := tests.DefaultNotifyContext(0, tc.DeferCleanup)
	// TODO: Fix GetNodeWebsocketURIs - function doesn't exist
	var wsURIs []string
	_ = wsURIs // wsURIs would be populated from GetNodeWebsocketURIs

	registry := metric.NewRegistry()
	metricsServer, err := tests.NewPrometheusServer(registry)
	require.NoError(err)
	tc.DeferCleanup(func() {
		require.NoError(metricsServer.Stop())
	})

	// TODO: Fix GetMonitoringLabels - method doesn't exist
	monitoringConfigFilePath, err := tmpnet.WritePrometheusSDConfig("load-test", tmpnet.SDConfig{
		Targets: []string{metricsServer.Address()},
		Labels:  map[string]string{"network": "test"},
	}, false)
	require.NoError(err, "failed to generate monitoring config file")

	tc.DeferCleanup(func() {
		require.NoError(
			os.Remove(monitoringConfigFilePath),
			"failed †o remove monitoring config file",
		)
	})

	workers := make([]load.Worker, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		workers[i] = load.Worker{
			PrivKey: keys[i].ToECDSA(),
			Client:  client,
		}
	}

	chainID, err := workers[0].Client.ChainID(ctx)
	require.NoError(err)

	randomTest, err := load.NewRandomTest(
		ctx,
		chainID,
		&workers[0],
		rand.NewSource(time.Now().UnixMilli()),
	)
	require.NoError(err)

	generator, err := load.NewLoadGenerator(
		workers,
		chainID,
		metricsNamespace,
		registry,
		randomTest,
	)
	require.NoError(err)

	generator.Run(ctx, log, loadTimeout, testTimeout)
}

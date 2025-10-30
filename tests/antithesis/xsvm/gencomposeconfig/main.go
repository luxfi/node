// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"go.uber.org/zap"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/antithesis"
	"github.com/luxfi/node/tests/fixture/subnet"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-xsvm"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	network := tmpnet.LocalNetworkOrPanic()
	network.Subnets = []*tmpnet.Subnet{
		subnet.NewXSVMOrPanic("xsvm", genesis.VMRQKey, network.Nodes...),
	}
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		tests.NewDefaultLogger("").Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
}

// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"github.com/luxfi/log"

	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/antithesis"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-luxgo"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	network := tmpnet.LocalNetworkOrPanic()
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		tests.NewDefaultLogger("").Fatal("failed to generate compose config",
			log.Error(err),
		)
		os.Exit(1)
	}
}

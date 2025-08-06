// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/luxfi/node/tests/antithesis"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-node"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	targetPath := os.Getenv("TARGET_PATH")
	if len(targetPath) == 0 {
		log.Error("TARGET_PATH environment variable not set")
	}

	imageTag := os.Getenv("IMAGE_TAG")
	if len(imageTag) == 0 {
		log.Error("IMAGE_TAG environment variable not set")
	}

	nodeImageName := fmt.Sprintf("%s-node:%s", baseImageName, imageTag)
	workloadImageName := fmt.Sprintf("%s-workload:%s", baseImageName, imageTag)

	network := tmpnet.LocalNetworkOrPanic()
	err := antithesis.GenerateComposeConfig(network, nodeImageName, workloadImageName, targetPath)
	if err != nil {
		log.Fatalf("failed to generate config for docker-compose: %s", err)
	}
}

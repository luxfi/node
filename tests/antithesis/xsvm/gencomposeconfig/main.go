// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/tests/antithesis"
	"github.com/luxfi/node/tests/fixture/subnet"
	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-xsvm"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	luxNodePath := os.Getenv("LUXD_PATH")
	if len(luxNodePath) == 0 {
		log.Error("LUXD_PATH environment variable not set")
	}

	pluginDir := os.Getenv("LUXD_PLUGIN_DIR")
	if len(pluginDir) == 0 {
		log.Error("LUXD_PLUGIN_DIR environment variable not set")
	}

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

	// Create a network with an xsvm subnet
	network := tmpnet.LocalNetworkOrPanic()
	network.Subnets = []*tmpnet.Subnet{
		subnet.NewXSVMOrPanic("xsvm", genesis.VMRQKey, network.Nodes...),
	}

	bootstrapVolumePath, err := antithesis.GetBootstrapVolumePath(targetPath)
	if err != nil {
		log.Fatalf("failed to get bootstrap volume path: %v", err)
	}

	if err := antithesis.InitBootstrapDB(network, luxNodePath, pluginDir, bootstrapVolumePath); err != nil {
		log.Fatalf("failed to initialize db volumes: %v", err)
	}

	if err := antithesis.GenerateComposeConfig(network, nodeImageName, workloadImageName, targetPath); err != nil {
		log.Fatalf("failed to generate config for docker-compose: %v", err)
	}
}

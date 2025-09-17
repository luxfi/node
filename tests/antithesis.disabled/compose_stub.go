// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !antithesis
// +build !antithesis

package antithesis

import (
	"errors"

	"github.com/luxfi/node/tests/fixture/tmpnet"
)

// GenerateComposeConfig is a stub for non-antithesis builds
func GenerateComposeConfig(
	network *tmpnet.Network,
	nodeImageName string,
	workloadImageName string,
	targetPath string,
) error {
	return errors.New("GenerateComposeConfig requires antithesis build tag")
}

// GetBootstrapVolumePath is a stub for non-antithesis builds
func GetBootstrapVolumePath(targetPath string) (string, error) {
	return "", errors.New("GetBootstrapVolumePath requires antithesis build tag")
}

// InitBootstrapDB is a stub for non-antithesis builds
func InitBootstrapDB(network *tmpnet.Network, luxNodePath string, pluginDir string, destPath string) error {
	return errors.New("InitBootstrapDB requires antithesis build tag")
}

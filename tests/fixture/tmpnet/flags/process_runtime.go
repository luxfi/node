// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/luxfi/node/tests/fixture/tmpnet"
)

const (
	processRuntime   = "process"
	processDocPrefix = "[process runtime] "

	nodePathFlag = "node-path"
)

var errLuxGoRequired = fmt.Errorf("--%s or %s are required", nodePathFlag, tmpnet.LuxGoPathEnvName)

type processRuntimeVars struct {
	config tmpnet.ProcessRuntimeConfig
}

func (v *processRuntimeVars) registerWithFlag() {
	v.register(flag.StringVar, flag.BoolVar)
}

func (v *processRuntimeVars) registerWithFlagSet(flagSet *pflag.FlagSet) {
	v.register(flagSet.StringVar, flagSet.BoolVar)
}

func (v *processRuntimeVars) register(stringVar varFunc[string], boolVar varFunc[bool]) {
	stringVar(
		&v.config.LuxGoPath,
		nodePathFlag,
		os.Getenv(tmpnet.LuxGoPathEnvName),
		processDocPrefix+fmt.Sprintf(
			"The node executable path. Also possible to configure via the %s env variable.",
			tmpnet.LuxGoPathEnvName,
		),
	)
	stringVar(
		&v.config.PluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.LuxGoPluginDirEnvName, os.ExpandEnv("$HOME/.node/plugins")),
		processDocPrefix+fmt.Sprintf(
			"The dir containing VM plugins. Also possible to configure via the %s env variable.",
			tmpnet.LuxGoPluginDirEnvName,
		),
	)
	boolVar(
		&v.config.ReuseDynamicPorts,
		"reuse-dynamic-ports",
		false,
		processDocPrefix+"Whether to attempt to reuse dynamically allocated ports across node restarts.",
	)
}

func (v *processRuntimeVars) getProcessRuntimeConfig() (*tmpnet.ProcessRuntimeConfig, error) {
	if err := v.validate(); err != nil {
		return nil, err
	}
	return &v.config, nil
}

func (v *processRuntimeVars) validate() error {
	path := v.config.LuxGoPath

	if len(path) == 0 {
		return errLuxGoRequired
	}

	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("--%s (%s) not found: %w", nodePathFlag, path, err)
		}
		return nil
	}

	// A relative path must be resolvable to an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf(
			"--%s (%s) is a relative path but its absolute path cannot be determined: %w",
			nodePathFlag,
			path,
			err,
		)
	}

	// The absolute path must exist
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf(
			"--%s (%s) is a relative path but its absolute path (%s) is not found: %w",
			nodePathFlag,
			path,
			absPath,
			err,
		)
	}
	return nil
}

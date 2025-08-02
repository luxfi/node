// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/luxfi/node/v2/tests/fixture/tmpnet"
)

const (
	processRuntime   = "process"
	processDocPrefix = "[process runtime] "

	luxdPathFlag = "luxd-path"
)

var errLuxRequired = fmt.Errorf("--%s or %s are required", luxdPathFlag, tmpnet.LuxPathEnvName)

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
		&v.config.LuxPath,
		luxdPathFlag,
		os.Getenv(tmpnet.LuxPathEnvName),
		processDocPrefix+fmt.Sprintf(
			"The luxd executable path. Also possible to configure via the %s env variable.",
			tmpnet.LuxPathEnvName,
		),
	)
	stringVar(
		&v.config.PluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.LuxPluginDirEnvName, os.ExpandEnv("$HOME/.luxd/plugins")),
		processDocPrefix+fmt.Sprintf(
			"The dir containing VM plugins. Also possible to configure via the %s env variable.",
			tmpnet.LuxPluginDirEnvName,
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
	path := v.config.LuxPath

	if len(path) == 0 {
		return errLuxRequired
	}

	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("--%s (%s) not found: %w", luxdPathFlag, path, err)
		}
		return nil
	}

	// A relative path must be resolvable to an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf(
			"--%s (%s) is a relative path but its absolute path cannot be determined: %w",
			luxdPathFlag,
			path,
			err,
		)
	}

	// The absolute path must exist
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf(
			"--%s (%s) is a relative path but its absolute path (%s) is not found: %w",
			luxdPathFlag,
			path,
			absPath,
			err,
		)
	}
	return nil
}

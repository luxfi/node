// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
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

	luxdPathFlag = "luxd-path"
)

var errLuxdRequired = fmt.Errorf("--%s or %s are required", luxdPathFlag, tmpnet.LuxdPathEnvName)

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
		&v.config.LuxdPath,
		luxdPathFlag,
		os.Getenv(tmpnet.LuxdPathEnvName),
		processDocPrefix+fmt.Sprintf(
			"The luxd executable path. Also possible to configure via the %s env variable.",
			tmpnet.LuxdPathEnvName,
		),
	)
	stringVar(
		&v.config.PluginDir,
		"plugin-dir",
		tmpnet.GetEnvWithDefault(tmpnet.LuxdPluginDirEnvName, os.ExpandEnv("$HOME/.luxd/plugins")),
		processDocPrefix+fmt.Sprintf(
			"The dir containing VM plugins. Also possible to configure via the %s env variable.",
			tmpnet.LuxdPluginDirEnvName,
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
	path := v.config.LuxdPath

	if len(path) == 0 {
		return errLuxdRequired
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

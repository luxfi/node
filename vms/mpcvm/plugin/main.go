// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/luxfi/node/v2/version"
	"github.com/luxfi/node/v2/vms/rpcchainvm"

	"github.com/luxfi/node/v2/vms/mpcvm/plugin/runner"
)

func main() {
	printVersion := false
	for _, arg := range os.Args {
		if arg == "-v" || arg == "--version" {
			printVersion = true
			break
		}
	}
	if printVersion {
		fmt.Printf("mpcvm %s [database=%s, node=%s]\n", Version, version.Current, version.Current)
		os.Exit(0)
	}

	runner.Run(versionEnabled, rpcchainvm.New)
}
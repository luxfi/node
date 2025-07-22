// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/rpcchainvm"

	"github.com/luxfi/node/mvm/plugin/runner"
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
		fmt.Printf("mvm %s [database=%s, node=%s]\n", Version, version.Current, version.Current)
		os.Exit(0)
	}

	runner.Run(versionEnabled, rpcchainvm.New)
}
// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package run

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/luxfi/node/v2/vms/example/xsvm"
	"github.com/luxfi/node/v2/vms/rpcchainvm"
)

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "xsvm",
		Short: "Runs an XSVM plugin",
		RunE:  runFunc,
	}
}

func runFunc(*cobra.Command, []string) error {
	return rpcchainvm.Serve(context.Background(), &xsvm.VM{})
}

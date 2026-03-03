//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package run

import (
	"github.com/spf13/cobra"

	"github.com/luxfi/node/vms/example/xsvm"
	"github.com/luxfi/node/vms/rpcchainvm"
)

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "xsvm",
		Short: "Runs an XSVM plugin",
		RunE:  runFunc,
	}
}

func runFunc(*cobra.Command, []string) error {
	// xsvm.VM does not yet implement the current consensus ChainVM interface.
	// The plugin is a no-op until the interface alignment is complete.
	_ = rpcchainvm.Serve
	_ = &xsvm.VM{}
	return nil
}

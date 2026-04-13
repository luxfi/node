// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// luxd — Lux Network node daemon.
//
// Entry point for the Lux primary-network node binary. Loads configuration
// from CLI flags + config files via Viper, constructs a [node.Node], and
// dispatches the main event loop.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/luxfi/log"
	"github.com/luxfi/node/config"
	"github.com/luxfi/node/node"
	"github.com/luxfi/node/version"
	"github.com/luxfi/sys/ulimit"
)

const Header = `
▼▼▼▼▼
 ▼▼▼
  ▼
`

func main() {
	if exitCode := run(os.Args[1:]); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func run(args []string) int {
	// --version short-circuit
	for _, a := range args {
		if a == "--version" || a == "-v" {
			fmt.Println(version.CurrentApp.String())
			return 0
		}
	}

	fmt.Print(Header)

	// Parse CLI + config file via Viper.
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "luxd: failed to parse config:", err)
		return 1
	}
	cfg, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "luxd: failed to load node config:", err)
		return 1
	}

	// Logger.
	infoLevel, _ := log.ToLevel("info")
	logFactory := log.NewFactoryWithConfig(log.Config{
		DisplayLevel: infoLevel,
		LogLevel:     infoLevel,
	})
	logger, err := logFactory.Make("node")
	if err != nil {
		fmt.Fprintln(os.Stderr, "luxd: failed to construct logger:", err)
		return 1
	}

	// Bump file descriptor limit early; node + its plugins open many sockets.
	if err := ulimit.Set(ulimit.DefaultFDLimit, logger); err != nil {
		logger.Error("failed to set fd limit", "error", err)
		return 1
	}

	// Construct the node.
	n, err := node.New(&cfg, logFactory, logger)
	if err != nil {
		logger.Error("failed to construct node", "error", err)
		return 1
	}

	// Run dispatch loop with signal-driven shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		logger.Info("received shutdown signal")
		n.Shutdown(0)
	}()

	if err := n.Dispatch(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("dispatch returned error", "error", err)
		return 1
	}
	return n.ExitCode()
}

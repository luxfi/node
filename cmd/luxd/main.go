// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luxfi/node2/host"
)

const version = "v2.0.0-trilang"

const usage = `luxd — Lux Validator Daemon (Go Runtime)

Usage:
  luxd [options]

Options:
  --data <dir>         where validator keys and journal live (default: .lux)
  --rpc <addr>         JSON-RPC listen address               (default: 127.0.0.1:9650)
  --stake <addr>       validator mesh address                (default: 127.0.0.1:9651)
  --network <name>     mainnet | testnet | localnet          (default: localnet)
  --archive-rpc <url>  proxy historical state and EVM calls from this archive node
  --light              light node mode: only keep frontier state
  --produce <ms>       how often to look for work in ms      (default: 200)
  --version            print version and exit
  -h, --help           print help and exit
`

func main() {
	var (
		dataDir     = flag.String("data", ".lux", "Data directory")
		rpcAddr     = flag.String("rpc", "127.0.0.1:9650", "RPC listen address")
		stakeAddr   = flag.String("stake", "127.0.0.1:9651", "Staking listen address")
		networkName = flag.String("network", "localnet", "Network name")
		archiveRPC  = flag.String("archive-rpc", "", "Archive RPC proxy URL")
		lightMode   = flag.Bool("light", false, "Light node mode")
		produceMs   = flag.Int("produce", 200, "Block production cadence in ms")
		showVersion = flag.Bool("version", false, "Show version")
	)

	flag.Usage = func() {
		fmt.Print(usage)
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("luxd %s (linux/amd64, Go runtime, zero ava-labs)\n", version)
		os.Exit(0)
	}

	// Environment variable fallback
	if *archiveRPC == "" {
		if env := os.Getenv("LUX_ARCHIVE_RPC"); env != "" {
			*archiveRPC = env
		} else if env := os.Getenv("HANZO_ARCHIVE_RPC"); env != "" {
			*archiveRPC = env
		}
	}

	cfg := host.Config{
		DataDir:      *dataDir,
		RPCAddr:      *rpcAddr,
		StakeAddr:    *stakeAddr,
		NetworkName:  *networkName,
		ArchiveRPC:   *archiveRPC,
		LightMode:    *lightMode,
		ProduceDelay: time.Duration(*produceMs) * time.Millisecond,
	}

	h, err := host.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "luxd: initialization failed: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "luxd: failed to start host: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("luxd %s [Go runtime]\n", version)
	fmt.Printf("  Node ID:     %s\n", h.NodeID())
	fmt.Printf("  Network:     %s\n", *networkName)
	fmt.Printf("  RPC server:  http://%s/v1/chain/{p,x,c,q,z}\n", *rpcAddr)
	fmt.Printf("  Staking mesh: %s (TLS 1.3)\n", *stakeAddr)
	if *archiveRPC != "" {
		fmt.Printf("  Archive proxy: %s\n", *archiveRPC)
	}
	fmt.Println("  Chains online: [P-Chain, X-Chain, C-Chain, Q-Chain, Z-Chain]")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("\nReceived signal %s; shutting down gracefully...\n", sig)

	if err := h.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "luxd: shutdown error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("luxd stopped cleanly.")
}

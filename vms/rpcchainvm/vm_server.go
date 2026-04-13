//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcchainvm provides the RPC infrastructure for Chain VMs.
// This file provides backward-compatible type aliases to the shared implementation
// in github.com/luxfi/vm/rpc/chain.
package rpcchainvm

import (
	"github.com/luxfi/atomic"
	enginechain "github.com/luxfi/vm/chain"
	rpcchain "github.com/luxfi/vm/rpc/chain"
)

// VMServer is a type alias for backward compatibility.
// The actual implementation is in github.com/luxfi/vm/rpc/chain.Server.
type VMServer = rpcchain.Server

// NewServer creates a new VMServer for the given ChainVM.
// This delegates to the shared implementation in github.com/luxfi/vm/rpc/chain.
func NewServer(vm enginechain.ChainVM, allowShutdown *atomic.Atomic[bool]) *VMServer {
	return rpcchain.NewServer(vm, allowShutdown)
}

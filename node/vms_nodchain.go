// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !dchain

package node

// appendDChainVM is a no-op in the public (non-dchain) build: the D-Chain
// (DEXVM) proxy is NOT linked, so `go build ./...` of luxd does not import
// github.com/luxfi/chains/dexvm at all and the entire proxy/D-Chain tree is
// absent from the public binary. The venue build (-tags dchain) links it via
// vms_dchain.go.
func appendDChainVM(_ *Node, _ *[]vmEntry) {}

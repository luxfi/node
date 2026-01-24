//go:build !allvms

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

// registerOptionalVMs is a no-op in minimal build
// Use -tags=allvms for full VM support
func (n *Node) registerOptionalVMs() error {
	n.Log.Info("Minimal build - optional VMs not included")
	n.Log.Info("Build with -tags=allvms for: Threshold, ZK, DEX, Quantum, AI, Bridge, Graph, Key, Oracle, Relay, Identity")
	return nil
}

func logOptionalVMs(n *Node) {
	n.Log.Info("(Optional VMs not included in minimal build)")
}

const optionalVMCount = 0

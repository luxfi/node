// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

// registerOptionalVMs is a no-op. All non-core VMs (Q, A, B, T, Z, G, D, K,
// O, R, I chains) are loaded as external plugins from --plugin-dir. Install
// them via lpm or place binaries in ~/.lux/plugins/<vmid>.
func (n *Node) registerOptionalVMs() error {
	n.Log.Info("Optional VMs load from plugin-dir (no in-process registration)")
	return nil
}

func logOptionalVMs(n *Node) {
	n.Log.Info("Optional VMs loaded as external plugins from --plugin-dir")
}

const optionalVMCount = 0

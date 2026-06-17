// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build dchain

package node

import "github.com/luxfi/chains/dexvm"

// appendDChainVM links the D-Chain (DEXVM) proxy and appends its factory to the
// optional-VM registration set. This file compiles ONLY under -tags dchain, so
// the chains/dexvm import — and the entire proxy/D-Chain tree behind it — is
// absent from the public OSS luxd binary (public-build purity).
//
// Defense in depth: even a dchain-tagged binary will not INSTANTIATE the chain
// unless D-Chain is present in genesis (the genesis-gate in node.go). Three
// independent layers gate the venue: public can't link it; venue without
// D-Chain genesis links-but-doesn't-run it; venue + genesis runs it.
func appendDChainVM(n *Node, entries *[]vmEntry) {
	*entries = append(*entries, vmEntry{"DEXVM (D-Chain)", dexvm.VMID, &dexvm.Factory{}})
	n.Log.Info("D-Chain (DEXVM) proxy linked (dchain build)")
}

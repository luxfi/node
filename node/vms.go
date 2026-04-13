// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/aivm"
	"github.com/luxfi/node/vms/bridgevm"
	"github.com/luxfi/node/vms/dexvm"
	"github.com/luxfi/node/vms/graphvm"
	"github.com/luxfi/node/vms/identityvm"
	"github.com/luxfi/node/vms/keyvm"
	"github.com/luxfi/node/vms/oraclevm"
	"github.com/luxfi/node/vms/quantumvm"
	"github.com/luxfi/node/vms/relayvm"
	"github.com/luxfi/node/vms/thresholdvm"
	"github.com/luxfi/node/vms/zkvm"
)

type vmEntry struct {
	name    string
	id      ids.ID
	factory vms.Factory
}

// registerOptionalVMs registers the 11 optional VMs (A/B/D/G/I/K/O/Q/R/T/Z).
// Primary network (P/X/C) is registered separately.
// Session (S-Chain) is a standalone plugin at github.com/luxfi/session/plugin.
func (n *Node) registerOptionalVMs() error {
	entries := []vmEntry{
		{"AIVM (A-Chain)", aivm.VMID, &aivm.Factory{}},
		{"BridgeVM (B-Chain)", bridgevm.VMID, &bridgevm.Factory{}},
		{"DEXVM (D-Chain)", dexvm.VMID, &dexvm.Factory{}},
		{"GraphVM (G-Chain)", graphvm.VMID, &graphvm.Factory{}},
		{"IdentityVM (I-Chain)", identityvm.VMID, &identityvm.Factory{}},
		{"KeyVM (K-Chain)", keyvm.VMID, &keyvm.Factory{}},
		{"OracleVM (O-Chain)", oraclevm.VMID, &oraclevm.Factory{}},
		{"QuantumVM (Q-Chain)", quantumvm.VMID, &quantumvm.Factory{}},
		{"RelayVM (R-Chain)", relayvm.VMID, &relayvm.Factory{}},
		{"ThresholdVM (T-Chain)", thresholdvm.VMID, &thresholdvm.Factory{}},
		{"ZKVM (Z-Chain)", zkvm.VMID, &zkvm.Factory{}},
	}

	registered := 0
	for _, e := range entries {
		if err := n.VMManager.RegisterFactory(context.Background(), e.id, e.factory); err != nil {
			n.Log.Warn("Failed to register VM", "name", e.name, "error", err)
			continue
		}
		n.Log.Info("VM registered", "name", e.name)
		registered++
	}

	n.Log.Info("Optional VMs registered", "count", registered)
	return nil
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/aivm"
	bvm "github.com/luxfi/node/vms/bridgevm"
	"github.com/luxfi/node/vms/dexvm"
	"github.com/luxfi/node/vms/identityvm"
	"github.com/luxfi/node/vms/keyvm"
	"github.com/luxfi/node/vms/oraclevm"
	qvm "github.com/luxfi/node/vms/quantumvm"
	"github.com/luxfi/node/vms/relayvm"
	"github.com/luxfi/node/vms/servicenodevm"
	"github.com/luxfi/node/vms/teleportvm"
	tvm "github.com/luxfi/node/vms/thresholdvm"
	zvm "github.com/luxfi/node/vms/zkvm"
)

type vmEntry struct {
	name    string
	id      ids.ID
	factory vms.Factory
}

func (n *Node) registerOptionalVMs() error {
	entries := []vmEntry{
		{"QuantumVM (Q-Chain)", qvm.VMID, &qvm.Factory{}},
		{"AIVM (A-Chain)", aivm.VMID, &aivm.Factory{}},
		{"BridgeVM (B-Chain)", bvm.VMID, &bvm.Factory{}},
		{"DEXVM (D-Chain)", dexvm.VMID, &dexvm.Factory{}},
		{"IdentityVM (I-Chain)", identityvm.VMID, &identityvm.Factory{}},
		{"KeyVM (K-Chain)", keyvm.VMID, &keyvm.Factory{}},
		{"OracleVM (O-Chain)", oraclevm.VMID, &oraclevm.Factory{}},
		{"RelayVM (R-Chain)", relayvm.VMID, &relayvm.Factory{}},
		{"ServiceNodeVM (S-Chain)", servicenodevm.VMID, &servicenodevm.Factory{}},
		{"TeleportVM (T-Chain)", teleportvm.VMID, &teleportvm.Factory{}},
		{"ThresholdVM", tvm.VMID, &tvm.Factory{}},
		{"ZKVM (Z-Chain)", zvm.VMID, &zvm.Factory{}},
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

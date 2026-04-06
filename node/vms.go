// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"

	"github.com/luxfi/constants"
	"github.com/luxfi/node/vms/aivm"
	bvm "github.com/luxfi/node/vms/bridgevm"
	dexvm "github.com/luxfi/node/vms/dexvm"
	graphvm "github.com/luxfi/node/vms/graphvm"
	ivm "github.com/luxfi/node/vms/identityvm"
	keyvm "github.com/luxfi/node/vms/keyvm"
	ovm "github.com/luxfi/node/vms/oraclevm"
	qvm "github.com/luxfi/node/vms/quantumvm"
	rvm "github.com/luxfi/node/vms/relayvm"
	tvm "github.com/luxfi/node/vms/thresholdvm"
	zvm "github.com/luxfi/node/vms/zkvm"
)

// registerOptionalVMs registers all chain VMs beyond P/X/C
func (n *Node) registerOptionalVMs() error {
	// Register Q-Chain VM (QuantumVM) - Post-quantum cryptography
	n.Log.Info("Registering Q-Chain VM (Quantum)", "vmID", constants.QuantumVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.QuantumVMID, &qvm.Factory{}); err != nil {
		n.Log.Error("Failed to register Q-Chain VM", "error", err)
		return err
	}

	// Register A-Chain VM (AIVM) - AI/ML inference
	n.Log.Info("Registering A-Chain VM (AI)", "vmID", constants.AIVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.AIVMID, &aivm.Factory{}); err != nil {
		n.Log.Error("Failed to register A-Chain VM", "error", err)
		return err
	}

	// Register B-Chain VM (Bridge VM) - Cross-chain bridge
	n.Log.Info("Registering B-Chain VM (Bridge)", "vmID", constants.BridgeVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.BridgeVMID, &bvm.Factory{}); err != nil {
		n.Log.Error("Failed to register B-Chain VM", "error", err)
		return err
	}

	// Register T-Chain VM (Threshold VM) - Threshold signatures
	n.Log.Info("Registering T-Chain VM (Threshold)", "vmID", constants.ThresholdVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.ThresholdVMID, &tvm.Factory{}); err != nil {
		n.Log.Error("Failed to register T-Chain VM", "error", err)
		return err
	}

	// Register Z-Chain VM (ZKVM) - Zero-Knowledge proofs
	n.Log.Info("Registering Z-Chain VM (ZK)", "vmID", constants.ZKVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.ZKVMID, &zvm.Factory{}); err != nil {
		n.Log.Error("Failed to register Z-Chain VM", "error", err)
		return err
	}

	// Register G-Chain VM (GraphVM) - GraphQL data layer
	n.Log.Info("Registering G-Chain VM (Graph)", "vmID", constants.GraphVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.GraphVMID, &graphvm.Factory{}); err != nil {
		n.Log.Error("Failed to register G-Chain VM", "error", err)
		return err
	}

	// Register D-Chain VM (DexVM) - Decentralized Exchange
	n.Log.Info("Registering D-Chain VM (DEX)", "vmID", constants.DexVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.DexVMID, &dexvm.Factory{}); err != nil {
		n.Log.Error("Failed to register D-Chain VM", "error", err)
		return err
	}

	// Register K-Chain VM (KMSVM) - Key Management Service
	n.Log.Info("Registering K-Chain VM (Key)", "vmID", constants.KeyVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.KeyVMID, keyvm.NewDefaultFactory()); err != nil {
		n.Log.Error("Failed to register K-Chain VM", "error", err)
		return err
	}

	// Register O-Chain VM (OracleVM) - Oracle/Off-chain Data
	n.Log.Info("Registering O-Chain VM (Oracle)", "vmID", constants.OracleVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.OracleVMID, &ovm.Factory{}); err != nil {
		n.Log.Error("Failed to register O-Chain VM", "error", err)
		return err
	}

	// Register R-Chain VM (RelayVM) - Cross-chain Relay
	n.Log.Info("Registering R-Chain VM (Relay)", "vmID", constants.RelayVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.RelayVMID, &rvm.Factory{}); err != nil {
		n.Log.Error("Failed to register R-Chain VM", "error", err)
		return err
	}

	// Register I-Chain VM (IdentityVM) - Decentralized Identity
	n.Log.Info("Registering I-Chain VM (Identity)", "vmID", constants.IdentityVMID)
	if err := n.VMManager.RegisterFactory(context.TODO(), constants.IdentityVMID, &ivm.Factory{}); err != nil {
		n.Log.Error("Failed to register I-Chain VM", "error", err)
		return err
	}

	n.Log.Info("All VMs registered")
	return nil
}

func logOptionalVMs(n *Node) {
	n.Log.Info("Q-Chain (Quantum):  Post-quantum security", "vmID", constants.QuantumVMID)
	n.Log.Info("A-Chain (AI):       AI/ML inference", "vmID", constants.AIVMID)
	n.Log.Info("B-Chain (Bridge):   Cross-chain bridge", "vmID", constants.BridgeVMID)
	n.Log.Info("T-Chain (Threshold): Threshold signatures", "vmID", constants.ThresholdVMID)
	n.Log.Info("Z-Chain (ZK):       Zero-knowledge proofs", "vmID", constants.ZKVMID)
	n.Log.Info("G-Chain (Graph):    GraphQL data layer", "vmID", constants.GraphVMID)
	n.Log.Info("D-Chain (DEX):      Decentralized exchange", "vmID", constants.DexVMID)
	n.Log.Info("K-Chain (Key):      Key management service", "vmID", constants.KeyVMID)
	n.Log.Info("O-Chain (Oracle):   Oracle/off-chain data", "vmID", constants.OracleVMID)
	n.Log.Info("R-Chain (Relay):    Cross-chain relay", "vmID", constants.RelayVMID)
	n.Log.Info("I-Chain (Identity): Decentralized identity", "vmID", constants.IdentityVMID)
}

const optionalVMCount = 11

package rpcchainvm

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/consensus/validators"
)

// Context is the node-specific context for RPC chain VM
type Context struct {
	NetworkID       uint32
	NetID        ids.ID
	ChainID         ids.ID
	NodeID          ids.NodeID
	PublicKey       *bls.PublicKey
	NetworkUpgrades upgrade.Config

	XChainID       ids.ID
	CChainID       ids.ID
	LUXAssetID     ids.ID
	ChainDataDir   string

	Log            log.Logger
	SharedMemory   atomic.SharedMemory
	BCLookup       ids.AliaserReader
	Metrics        metrics.MultiGatherer
	WarpSigner     warp.Signer
	ValidatorState validators.State
}

// ToConsensusContext converts node Context to consensus Context
// This is explicit - we know exactly what we're doing
func (c *Context) ToConsensusContext() interface{} {
	// Return as interface{} - consensus layer decides how to use it
	// We don't pretend the types match - they don't
	return c
}

// PublicKeyBytes returns the public key as bytes
func (c *Context) PublicKeyBytes() []byte {
	if c.PublicKey == nil {
		return nil
	}
	return bls.PublicKeyToCompressedBytes(c.PublicKey)
}

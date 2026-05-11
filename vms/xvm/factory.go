// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/node/vms/xvm/config"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct {
	config.Config

	// SecurityProfile pins the chain-wide credential-admission policy
	// resolved at node bootstrap from the genesis SecurityProfile block
	// (F102). The X-chain VM threads this into its mempool builder via
	// SetAuthPolicy so strict-PQ chains refuse classical secp256k1
	// credentials at gossip time. Nil for legacy (classical-compat)
	// networks that pre-date the locked-profile pin.
	SecurityProfile *consensusconfig.ChainSecurityProfile

	// ClassicalCompatRegistry names the allow-list of legacy operators
	// that may still post classical secp256k1 credentials under a
	// classical-compat fork profile. Nil for strict-PQ.
	ClassicalCompatRegistry auth.ClassicalCompatRegistry
}

func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{
		Config:                  f.Config,
		securityProfile:         f.SecurityProfile,
		classicalCompatRegistry: f.ClassicalCompatRegistry,
	}, nil
}

// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p/lp118"
	"github.com/luxfi/node/vms/platformvm/warp"
	consensuscore "github.com/luxfi/consensus/core"
	luxWarp "github.com/luxfi/warp"
)

var _ lp118.Verifier = (*lp118Verifier)(nil)

// lp118Verifier allows signing all warp messages
type lp118Verifier struct{}

func (lp118Verifier) Verify(context.Context, *luxWarp.UnsignedMessage, []byte) *consensuscore.AppError {
	return nil
}

// xsvmWarpSignerAdapter adapts internal warp.Signer to lp118.Signer (external warp)
type xsvmWarpSignerAdapter struct {
	signer interface {
		Sign(*warp.UnsignedMessage) ([]byte, error)
	}
}

// Sign implements lp118.Signer interface
func (a *xsvmWarpSignerAdapter) Sign(msg *luxWarp.UnsignedMessage) ([]byte, error) {
	// Convert external warp message to internal warp message
	var sourceChainID ids.ID
	copy(sourceChainID[:], msg.SourceChainID)
	internalMsg, err := warp.NewUnsignedMessage(msg.NetworkID, sourceChainID, msg.Payload)
	if err != nil {
		return nil, err
	}
	// Sign using internal signer and return raw signature bytes
	return a.signer.Sign(internalMsg)
}

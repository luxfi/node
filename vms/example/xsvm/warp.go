// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"

	"github.com/luxfi/p2p/lp118"
	"github.com/luxfi/node/vms/platformvm/warp"
	luxWarp "github.com/luxfi/warp"
)

var _ lp118.Verifier = (*lp118Verifier)(nil)

// lp118Verifier allows signing all warp messages
type lp118Verifier struct{}

func (lp118Verifier) Verify(context.Context, *luxWarp.UnsignedMessage, []byte) error {
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
	// Convert external warp message (luxWarp) to internal warp message (platformvm/warp)
	// msg.SourceChainID is already ids.ID type
	internalMsg, err := warp.NewUnsignedMessage(msg.NetworkID, msg.SourceChainID, msg.Payload)
	if err != nil {
		return nil, err
	}
	return a.signer.Sign(internalMsg)
}

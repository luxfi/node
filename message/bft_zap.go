//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import "github.com/luxfi/node/proto/p2p"

// newMessageBFT creates a BFT message wrapper (ZAP version)
func newMessageBFT(msg *p2p.BFT) *p2p.Message_BFT {
	return &p2p.Message_BFT{BFT: msg}
}

// extractBFT extracts the BFT message from the wrapper (ZAP version)
func extractBFT(msg *p2p.Message_BFT) *p2p.BFT {
	return msg.BFT
}

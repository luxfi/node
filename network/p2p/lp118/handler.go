// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package lp118 implements LP-118 message handling
package lp118

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/proto/pb/sdk"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// HandlerID is the protocol ID for LP-118
const HandlerID = 0x12345678 // Implementation note

// Handler handles ACP-118 messages
type Handler interface {
	// AppRequest handles an incoming request
	AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error)
}

// NoOpHandler is a no-op implementation of Handler
type NoOpHandler struct{}

// AppRequest returns an empty response
func (NoOpHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}

// CachedHandler implements a cached handler for LP-118
type CachedHandler struct {
	cache   cache.Cacher[ids.ID, []byte]
	backend interface{}
	signer  warp.Signer
}

// NewCachedHandler creates a new cached handler
func NewCachedHandler(cache cache.Cacher[ids.ID, []byte], backend interface{}, signer warp.Signer) Handler {
	return &CachedHandler{
		cache:   cache,
		backend: backend,
		signer:  signer,
	}
}

// AppRequest handles an incoming request with caching
func (h *CachedHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error) {
	req := &sdk.SignatureRequest{}
	if err := proto.Unmarshal(request, req); err != nil {
		return nil, err
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(req.Message)
	if err != nil {
		return nil, err
	}

	// Check cache
	messageID := unsignedMessage.ID()
	if signatureBytes, ok := h.cache.Get(messageID); ok {
		resp := &sdk.SignatureResponse{
			Signature: signatureBytes,
		}
		return proto.Marshal(resp)
	}

	// Verify if backend is a Verifier
	if verifier, ok := h.backend.(Verifier); ok {
		if appErr := verifier.Verify(ctx, unsignedMessage, req.Justification); appErr != nil {
			return nil, appErr
		}
	}

	// Sign the message
	signatureBytes, err := h.signer.Sign(unsignedMessage)
	if err != nil {
		return nil, err
	}

	h.cache.Put(messageID, signatureBytes)

	resp := &sdk.SignatureResponse{
		Signature: signatureBytes,
	}
	return proto.Marshal(resp)
}

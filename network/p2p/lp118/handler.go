// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package lp118 implements LP-118 message handling
package lp118

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/proto/pb/sdk"
	"github.com/luxfi/warp"
)

// HandlerID is the protocol ID for LP-118
const HandlerID = 0x12345678 // Implementation note

// Handler handles ACP-118 messages
type Handler interface {
	// AppRequest handles an incoming request
	AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error)
}

// Signer signs warp messages and returns the signature bytes
type Signer interface {
	// Sign signs an unsigned warp message and returns the signature bytes
	Sign(msg *warp.UnsignedMessage) ([]byte, error)
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
	signer  Signer
}

// NewCachedHandler creates a new cached handler
func NewCachedHandler(cache cache.Cacher[ids.ID, []byte], backend interface{}, signer Signer) Handler {
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

	// Check cache - convert []byte ID to ids.ID
	idBytes := unsignedMessage.ID()
	var messageID ids.ID
	copy(messageID[:], idBytes)
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
	if h.signer == nil {
		return nil, fmt.Errorf("signer is nil")
	}
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

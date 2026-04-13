// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zwarp

import (
	"context"
	"fmt"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// Server implements the ZAP server for warp signing
type Server struct {
	signer warp.Signer
}

// NewServer creates a new ZAP-based warp signer server
func NewServer(signer warp.Signer) *Server {
	return &Server{signer: signer}
}

// HandleMessage handles incoming ZAP messages
func (s *Server) HandleMessage(ctx context.Context, msgType zapwire.MessageType, payload []byte) ([]byte, error) {
	switch msgType {
	case zapwire.MsgWarpSign:
		return s.handleSign(ctx, payload)
	case zapwire.MsgWarpGetPublicKey:
		return s.handleGetPublicKey(ctx, payload)
	case zapwire.MsgWarpBatchSign:
		return s.handleBatchSign(ctx, payload)
	default:
		return nil, fmt.Errorf("zwarp server: unknown message type %d", msgType)
	}
}

func (s *Server) handleSign(ctx context.Context, payload []byte) ([]byte, error) {
	req := &zapwire.WarpSignRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return nil, fmt.Errorf("zwarp decode sign request: %w", err)
	}

	sourceChainID, err := ids.ToID(req.SourceChainID)
	if err != nil {
		return s.encodeSignError(fmt.Sprintf("invalid source chain ID: %v", err))
	}

	msg, err := warp.NewUnsignedMessage(req.NetworkID, sourceChainID, req.Payload)
	if err != nil {
		return s.encodeSignError(fmt.Sprintf("invalid message: %v", err))
	}

	sig, err := s.signer.Sign(msg)
	if err != nil {
		return s.encodeSignError(err.Error())
	}

	resp := &zapwire.WarpSignResponse{
		Signature: sig,
	}
	buf := zapwire.GetBuffer()
	resp.Encode(buf)
	// Copy bytes before returning buffer to pool
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	zapwire.PutBuffer(buf)
	return result, nil
}

func (s *Server) encodeSignError(errMsg string) ([]byte, error) {
	resp := &zapwire.WarpSignResponse{
		Error: errMsg,
	}
	buf := zapwire.GetBuffer()
	resp.Encode(buf)
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	zapwire.PutBuffer(buf)
	return result, nil
}

func (s *Server) handleGetPublicKey(ctx context.Context, payload []byte) ([]byte, error) {
	// Get public key if signer supports it
	type publicKeyGetter interface {
		PublicKey() []byte
	}

	resp := &zapwire.WarpGetPublicKeyResponse{}

	if getter, ok := s.signer.(publicKeyGetter); ok {
		resp.PublicKey = getter.PublicKey()
	} else {
		resp.Error = "signer does not support public key retrieval"
	}

	buf := zapwire.GetBuffer()
	resp.Encode(buf)
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	zapwire.PutBuffer(buf)
	return result, nil
}

func (s *Server) handleBatchSign(ctx context.Context, payload []byte) ([]byte, error) {
	req := &zapwire.WarpBatchSignRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return nil, fmt.Errorf("zwarp decode batch sign request: %w", err)
	}

	resp := &zapwire.WarpBatchSignResponse{
		Signatures: make([][]byte, len(req.Messages)),
		Errors:     make([]string, len(req.Messages)),
	}

	for i, msgReq := range req.Messages {
		sourceChainID, err := ids.ToID(msgReq.SourceChainID)
		if err != nil {
			resp.Errors[i] = fmt.Sprintf("invalid source chain ID: %v", err)
			continue
		}

		msg, err := warp.NewUnsignedMessage(msgReq.NetworkID, sourceChainID, msgReq.Payload)
		if err != nil {
			resp.Errors[i] = fmt.Sprintf("invalid message: %v", err)
			continue
		}

		sig, err := s.signer.Sign(msg)
		if err != nil {
			resp.Errors[i] = err.Error()
			continue
		}

		resp.Signatures[i] = sig
	}

	buf := zapwire.GetBuffer()
	resp.Encode(buf)
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	zapwire.PutBuffer(buf)
	return result, nil
}

// Handler returns a zapwire.HandlerFunc that processes warp messages
func (s *Server) Handler() zapwire.HandlerFunc {
	return func(ctx context.Context, msgType zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
		respPayload, err := s.HandleMessage(ctx, msgType, payload)
		if err != nil {
			return msgType | zapwire.MsgResponseFlag, nil, err
		}
		return msgType | zapwire.MsgResponseFlag, respPayload, nil
	}
}

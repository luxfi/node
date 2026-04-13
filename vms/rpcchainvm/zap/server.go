// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"errors"
	"fmt"
	"net"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/log"
	"github.com/luxfi/vm"
)

// Server wraps a VM and serves it over ZAP transport
type Server struct {
	vm       vm.VM
	listener *zapwire.Listener
	server   *zapwire.Server
	logger   log.Logger
}

// NewServer creates a new ZAP server for a VM
func NewServer(v vm.VM, logger log.Logger) *Server {
	return &Server{
		vm:     v,
		logger: logger,
	}
}

// Listen starts listening on the specified address
func (s *Server) Listen(addr string, config *zapwire.Config) error {
	listener, err := zapwire.Listen(addr, config)
	if err != nil {
		return err
	}

	s.listener = listener
	s.server = zapwire.NewServer(listener, zapwire.HandlerFunc(s.handle))

	return nil
}

// Addr returns the listener's address
func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve starts serving requests
func (s *Server) Serve(ctx context.Context) error {
	if s.server == nil {
		return errors.New("server not initialized - call Listen first")
	}
	return s.server.Serve(ctx)
}

// Close closes the server
func (s *Server) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *Server) handle(ctx context.Context, msgType zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
	switch msgType {
	case zapwire.MsgInitialize:
		return s.handleInitialize(ctx, payload)
	case zapwire.MsgShutdown:
		return s.handleShutdown(ctx, payload)
	case zapwire.MsgParseBlock:
		return s.handleParseBlock(ctx, payload)
	case zapwire.MsgGetBlock:
		return s.handleGetBlock(ctx, payload)
	case zapwire.MsgSetPreference:
		return s.handleSetPreference(ctx, payload)
	case zapwire.MsgBlockVerify:
		return s.handleBlockVerify(ctx, payload)
	case zapwire.MsgBlockAccept:
		return s.handleBlockAccept(ctx, payload)
	case zapwire.MsgBlockReject:
		return s.handleBlockReject(ctx, payload)
	case zapwire.MsgBatchedParseBlock:
		return s.handleBatchedParseBlock(ctx, payload)
	case zapwire.MsgGetAncestors:
		return s.handleGetAncestors(ctx, payload)
	case zapwire.MsgWaitForEvent:
		return s.handleWaitForEvent(ctx)
	default:
		return 0, nil, fmt.Errorf("unknown message type: %d", msgType)
	}
}

func (s *Server) handleInitialize(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.InitializeRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	s.logger.Info("ZAP Initialize called",
		"networkID", req.NetworkID,
		"chainID", fmt.Sprintf("%x", req.ChainID),
	)

	// Note: Actual initialization would require setting up the runtime
	// This is handled by the node's chain manager

	resp := &zapwire.InitializeResponse{
		LastAcceptedID:       make([]byte, 32),
		LastAcceptedParentID: make([]byte, 32),
		Height:               0,
		Bytes:                nil,
		Timestamp:            0,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgInitialize, buf.Bytes(), nil
}

func (s *Server) handleShutdown(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	if err := s.vm.Shutdown(ctx); err != nil {
		return 0, nil, err
	}
	return zapwire.MsgShutdown, nil, nil
}

func (s *Server) handleParseBlock(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.ParseBlockRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	blk, err := s.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		resp := &zapwire.BlockResponse{
			Err: errorToZAP(err),
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		return zapwire.MsgParseBlock, buf.Bytes(), nil
	}

	id := blk.ID()
	parentID := blk.Parent()
	resp := &zapwire.BlockResponse{
		ID:                id[:],
		ParentID:          parentID[:],
		Bytes:             blk.Bytes(),
		Height:            blk.Height(),
		Timestamp:         blk.Timestamp(),
		VerifyWithContext: false,
		Err:               zapwire.ErrorUnspecified,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgParseBlock, buf.Bytes(), nil
}

func (s *Server) handleGetBlock(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.GetBlockRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	var blkID [32]byte
	copy(blkID[:], req.ID)

	blk, err := s.vm.GetBlock(ctx, blkID)
	if err != nil {
		resp := &zapwire.BlockResponse{
			Err: errorToZAP(err),
		}
		buf := zapwire.GetBuffer()
		defer zapwire.PutBuffer(buf)
		resp.Encode(buf)
		return zapwire.MsgGetBlock, buf.Bytes(), nil
	}

	id := blk.ID()
	parentID := blk.Parent()
	resp := &zapwire.BlockResponse{
		ID:                id[:],
		ParentID:          parentID[:],
		Bytes:             blk.Bytes(),
		Height:            blk.Height(),
		Timestamp:         blk.Timestamp(),
		VerifyWithContext: false,
		Err:               zapwire.ErrorUnspecified,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgGetBlock, buf.Bytes(), nil
}

func (s *Server) handleSetPreference(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.SetPreferenceRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	var blkID [32]byte
	copy(blkID[:], req.ID)

	if err := s.vm.SetPreference(ctx, blkID); err != nil {
		return 0, nil, err
	}

	return zapwire.MsgSetPreference, nil, nil
}

func (s *Server) handleBlockVerify(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.BlockVerifyRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	// Parse and verify block
	blk, err := s.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		return 0, nil, err
	}

	if err := blk.Verify(ctx); err != nil {
		return 0, nil, err
	}

	resp := &zapwire.BlockVerifyResponse{
		Timestamp: blk.Timestamp(),
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgBlockVerify, buf.Bytes(), nil
}

func (s *Server) handleBlockAccept(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.BlockAcceptRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	var blkID [32]byte
	copy(blkID[:], req.ID)

	blk, err := s.vm.GetBlock(ctx, blkID)
	if err != nil {
		return 0, nil, err
	}

	if err := blk.Accept(ctx); err != nil {
		return 0, nil, err
	}

	return zapwire.MsgBlockAccept, nil, nil
}

func (s *Server) handleBlockReject(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.BlockRejectRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	var blkID [32]byte
	copy(blkID[:], req.ID)

	blk, err := s.vm.GetBlock(ctx, blkID)
	if err != nil {
		return 0, nil, err
	}

	if err := blk.Reject(ctx); err != nil {
		return 0, nil, err
	}

	return zapwire.MsgBlockReject, nil, nil
}

func (s *Server) handleBatchedParseBlock(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.BatchedParseBlockRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	resp := &zapwire.BatchedParseBlockResponse{
		Responses: make([]zapwire.BlockResponse, len(req.Requests)),
	}

	for i, blockBytes := range req.Requests {
		blk, err := s.vm.ParseBlock(ctx, blockBytes)
		if err != nil {
			resp.Responses[i] = zapwire.BlockResponse{
				Err: errorToZAP(err),
			}
			continue
		}

		id := blk.ID()
		parentID := blk.Parent()
		resp.Responses[i] = zapwire.BlockResponse{
			ID:                id[:],
			ParentID:          parentID[:],
			Bytes:             blk.Bytes(),
			Height:            blk.Height(),
			Timestamp:         blk.Timestamp(),
			VerifyWithContext: false,
			Err:               zapwire.ErrorUnspecified,
		}
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgBatchedParseBlock, buf.Bytes(), nil
}

func (s *Server) handleGetAncestors(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	req := &zapwire.GetAncestorsRequest{}
	if err := req.Decode(zapwire.NewReader(payload)); err != nil {
		return 0, nil, err
	}

	var blkID [32]byte
	copy(blkID[:], req.BlkID)

	ancestors := make([][]byte, 0, req.MaxBlocksNum)
	currentID := blkID

	for i := int32(0); i < req.MaxBlocksNum; i++ {
		blk, err := s.vm.GetBlock(ctx, currentID)
		if err != nil {
			break
		}
		ancestors = append(ancestors, blk.Bytes())
		currentID = blk.Parent()
	}

	resp := &zapwire.GetAncestorsResponse{
		BlksBytes: ancestors,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgGetAncestors, buf.Bytes(), nil
}

func (s *Server) handleWaitForEvent(ctx context.Context) (zapwire.MessageType, []byte, error) {
	// Type-assert VM to check if it supports WaitForEvent
	type waitForEventer interface {
		WaitForEvent(ctx context.Context) (block.Message, error)
	}

	wfe, ok := s.vm.(waitForEventer)
	if !ok {
		return 0, nil, fmt.Errorf("VM does not implement WaitForEvent")
	}

	msg, err := wfe.WaitForEvent(ctx)
	if err != nil {
		return 0, nil, err
	}

	resp := &zapwire.WaitForEventResponse{
		Message: uint8(msg.Type),
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	resp.Encode(buf)

	return zapwire.MsgWaitForEvent, buf.Bytes(), nil
}

func errorToZAP(err error) zapwire.Error {
	if err == nil {
		return zapwire.ErrorUnspecified
	}
	errStr := err.Error()
	if errStr == "not found" || errStr == "block not found" {
		return zapwire.ErrorNotFound
	}
	if errStr == "closed" || errStr == "vm closed" {
		return zapwire.ErrorClosed
	}
	return zapwire.ErrorUnspecified
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap provides ZAP transport implementation for VM RPC.
package zap

import (
	"context"
	"errors"
	"fmt"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/vm"
)

var (
	ErrNotConnected    = errors.New("zap: not connected")
	ErrInvalidResponse = errors.New("zap: invalid response")
)

// Client implements vm.VM over ZAP transport
type Client struct {
	conn   *zapwire.Conn
	logger log.Logger

	// Cached state from Initialize
	lastAcceptedID ids.ID
}

// NewClient creates a new ZAP-based VM client
func NewClient(conn *zapwire.Conn, logger log.Logger) *Client {
	return &Client{
		conn:   conn,
		logger: logger,
	}
}

// Dial connects to a ZAP VM server
func Dial(ctx context.Context, addr string, config *zapwire.Config) (*zapwire.Conn, error) {
	return zapwire.Dial(ctx, addr, config)
}

// Initialize implements vm.VM
func (c *Client) Initialize(ctx context.Context, init vm.Init) error {
	var networkID uint32
	var chainID, nodeID []byte
	if init.Runtime != nil {
		networkID = init.Runtime.NetworkID
		chainID = init.Runtime.ChainID[:]
		nodeID = init.Runtime.NodeID[:]
	}

	req := &zapwire.InitializeRequest{
		NetworkID:    networkID,
		ChainID:      chainID,
		NodeID:       nodeID,
		GenesisBytes: init.Genesis,
		UpgradeBytes: init.Upgrade,
		ConfigBytes:  init.Config,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	respType, respData, err := c.conn.Call(ctx, zapwire.MsgInitialize, buf.Bytes())
	if err != nil {
		return fmt.Errorf("zap initialize: %w", err)
	}

	if respType&^zapwire.MsgResponseFlag != zapwire.MsgInitialize {
		return ErrInvalidResponse
	}

	resp := &zapwire.InitializeResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return fmt.Errorf("zap decode initialize response: %w", err)
	}

	copy(c.lastAcceptedID[:], resp.LastAcceptedID)

	c.logger.Info("VM initialized via ZAP",
		"height", resp.Height,
		"lastAcceptedID", c.lastAcceptedID,
	)

	return nil
}

// Shutdown implements vm.VM
func (c *Client) Shutdown(ctx context.Context) error {
	_, _, err := c.conn.Call(ctx, zapwire.MsgShutdown, nil)
	if err != nil {
		c.logger.Warn("ZAP shutdown error", "error", err)
	}
	return c.conn.Close()
}

// ParseBlock implements vm.VM
func (c *Client) ParseBlock(ctx context.Context, blockBytes []byte) (vm.Block, error) {
	req := &zapwire.ParseBlockRequest{
		Bytes: blockBytes,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, respData, err := c.conn.Call(ctx, zapwire.MsgParseBlock, buf.Bytes())
	if err != nil {
		return nil, err
	}

	resp := &zapwire.BlockResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return nil, err
	}

	if resp.Err != zapwire.ErrorUnspecified {
		return nil, errorFromZAP(resp.Err)
	}

	var id, parentID ids.ID
	copy(id[:], resp.ID)
	copy(parentID[:], resp.ParentID)

	return &block{
		client:    c,
		id:        id,
		parentID:  parentID,
		bytes:     blockBytes, // Use original bytes
		height:    resp.Height,
		timestamp: resp.Timestamp,
	}, nil
}

// GetBlock implements vm.VM
func (c *Client) GetBlock(ctx context.Context, blkID ids.ID) (vm.Block, error) {
	req := &zapwire.GetBlockRequest{
		ID: blkID[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, respData, err := c.conn.Call(ctx, zapwire.MsgGetBlock, buf.Bytes())
	if err != nil {
		return nil, err
	}

	resp := &zapwire.BlockResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return nil, err
	}

	if resp.Err != zapwire.ErrorUnspecified {
		return nil, errorFromZAP(resp.Err)
	}

	var parentID ids.ID
	copy(parentID[:], resp.ParentID)

	return &block{
		client:    c,
		id:        blkID,
		parentID:  parentID,
		bytes:     resp.Bytes,
		height:    resp.Height,
		timestamp: resp.Timestamp,
	}, nil
}

// SetPreference implements vm.VM
func (c *Client) SetPreference(ctx context.Context, blkID ids.ID) error {
	req := &zapwire.SetPreferenceRequest{
		ID: blkID[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := c.conn.Call(ctx, zapwire.MsgSetPreference, buf.Bytes())
	return err
}

// LastAccepted implements vm.VM
func (c *Client) LastAccepted(ctx context.Context) (ids.ID, error) {
	return c.lastAcceptedID, nil
}

// Close closes the connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// block implements vm.Block
type block struct {
	client    *Client
	id        ids.ID
	parentID  ids.ID
	bytes     []byte
	height    uint64
	timestamp int64
}

func (b *block) ID() ids.ID       { return b.id }
func (b *block) Parent() ids.ID   { return b.parentID }
func (b *block) Bytes() []byte    { return b.bytes }
func (b *block) Height() uint64   { return b.height }
func (b *block) Timestamp() int64 { return b.timestamp }

func (b *block) Verify(ctx context.Context) error {
	req := &zapwire.BlockVerifyRequest{
		Bytes:           b.bytes,
		HasPChainHeight: false,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := b.client.conn.Call(ctx, zapwire.MsgBlockVerify, buf.Bytes())
	return err
}

func (b *block) Accept(ctx context.Context) error {
	req := &zapwire.BlockAcceptRequest{
		ID: b.id[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := b.client.conn.Call(ctx, zapwire.MsgBlockAccept, buf.Bytes())
	return err
}

func (b *block) Reject(ctx context.Context) error {
	req := &zapwire.BlockRejectRequest{
		ID: b.id[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := b.client.conn.Call(ctx, zapwire.MsgBlockReject, buf.Bytes())
	return err
}

func errorFromZAP(err zapwire.Error) error {
	switch err {
	case zapwire.ErrorUnspecified:
		return nil
	case zapwire.ErrorClosed:
		return errors.New("vm closed")
	case zapwire.ErrorNotFound:
		return errors.New("not found")
	case zapwire.ErrorStateSyncNotImplemented:
		return errors.New("state sync not implemented")
	default:
		return fmt.Errorf("unknown error: %d", err)
	}
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap provides ZAP transport implementation for VM RPC.
package zap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/version"
	"github.com/luxfi/vm/chain"
)

var (
	ErrNotConnected    = errors.New("zap: not connected")
	ErrInvalidResponse = errors.New("zap: invalid response")
)

// Compile-time check that Client implements chain.ChainVM
var _ chain.ChainVM = (*Client)(nil)

// Client implements chain.ChainVM over ZAP transport
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

// Initialize implements chain.ChainVM
func (c *Client) Initialize(ctx context.Context, init block.Init) error {
	var networkID uint32
	var chainID, nodeID, publicKey []byte
	var xChainID, cChainID, luxAssetID []byte
	var chainDataDir string
	var networkUpgrades zapwire.NetworkUpgrades

	if init.Runtime != nil {
		rt := init.Runtime
		networkID = rt.NetworkID
		chainID = rt.ChainID[:]
		nodeID = rt.NodeID[:]
		publicKey = rt.PublicKey
		xChainID = rt.XChainID[:]
		cChainID = rt.CChainID[:]
		luxAssetID = rt.XAssetID[:]
		chainDataDir = rt.ChainDataDir

		// Extract network upgrades if available
		if rt.NetworkUpgrades != nil {
			// NetworkUpgrades is an interface{}, we need to extract timestamps
			// For now, use defaults - the VM will use its own config
		}
	}

	req := &zapwire.InitializeRequest{
		NetworkID:       networkID,
		ChainID:         chainID,
		NodeID:          nodeID,
		PublicKey:       publicKey,
		XChainID:        xChainID,
		CChainID:        cChainID,
		XAssetID:      luxAssetID,
		ChainDataDir:    chainDataDir,
		GenesisBytes:    init.Genesis,
		UpgradeBytes:    init.Upgrade,
		ConfigBytes:     init.Config,
		NetworkUpgrades: networkUpgrades,
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

	// Check if this is an error response from the server
	if respType&zapwire.MsgResponseFlag != 0 {
		return fmt.Errorf("zap initialize: vm error: %s", string(respData))
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

// Shutdown implements chain.ChainVM
func (c *Client) Shutdown(ctx context.Context) error {
	_, _, err := c.conn.Call(ctx, zapwire.MsgShutdown, nil)
	if err != nil {
		c.logger.Warn("ZAP shutdown error", "error", err)
	}
	return c.conn.Close()
}

// SetState implements chain.ChainVM
func (c *Client) SetState(ctx context.Context, state uint32) error {
	req := &zapwire.SetStateRequest{
		State: zapwire.State(state),
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := c.conn.Call(ctx, zapwire.MsgSetState, buf.Bytes())
	return err
}

// Version implements chain.ChainVM
func (c *Client) Version(ctx context.Context) (string, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgVersion, nil)
	if err != nil {
		return "", err
	}

	resp := &zapwire.VersionResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return "", err
	}

	return resp.Version, nil
}

// BuildBlock implements chain.ChainVM
func (c *Client) BuildBlock(ctx context.Context) (block.Block, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgBuildBlock, nil)
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

	return &zapBlock{
		client:    c,
		id:        id,
		parentID:  parentID,
		bytes:     resp.Bytes,
		height:    resp.Height,
		timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// ParseBlock implements chain.ChainVM
func (c *Client) ParseBlock(ctx context.Context, blockBytes []byte) (block.Block, error) {
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

	return &zapBlock{
		client:    c,
		id:        id,
		parentID:  parentID,
		bytes:     blockBytes, // Use original bytes
		height:    resp.Height,
		timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// GetBlock implements chain.ChainVM
func (c *Client) GetBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
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

	return &zapBlock{
		client:    c,
		id:        blkID,
		parentID:  parentID,
		bytes:     resp.Bytes,
		height:    resp.Height,
		timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// SetPreference implements chain.ChainVM
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

// LastAccepted implements chain.ChainVM
func (c *Client) LastAccepted(ctx context.Context) (ids.ID, error) {
	return c.lastAcceptedID, nil
}

// NewHTTPHandler implements chain.ChainVM
func (c *Client) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgNewHTTPHandler, nil)
	if err != nil {
		return nil, err
	}

	resp := &zapwire.NewHTTPHandlerResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return nil, err
	}

	// If no handler address returned, the VM doesn't have an HTTP handler
	if resp.ServerAddr == "" {
		return nil, nil
	}

	// Return a reverse proxy handler to the VM's HTTP server
	// For now, return nil as complex HTTP proxying is typically handled differently
	c.logger.Debug("VM HTTP handler available", "addr", resp.ServerAddr)
	return nil, nil
}

// CreateHandlers calls the VM's CreateHandlers and returns reverse proxy handlers
func (c *Client) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgCreateHandlers, nil)
	if err != nil {
		return nil, fmt.Errorf("zap CreateHandlers: %w", err)
	}

	resp := &zapwire.CreateHandlersResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return nil, fmt.Errorf("zap decode CreateHandlers response: %w", err)
	}

	if len(resp.Handlers) == 0 {
		return nil, nil
	}

	handlers := make(map[string]http.Handler, len(resp.Handlers))
	for _, h := range resp.Handlers {
		if h.ServerAddr == "" {
			continue
		}

		// Parse the server address and create a reverse proxy
		targetURL, err := url.Parse("http://" + h.ServerAddr)
		if err != nil {
			c.logger.Warn("failed to parse handler address", "prefix", h.Prefix, "addr", h.ServerAddr, "error", err)
			continue
		}

		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		handlers[h.Prefix] = proxy

		c.logger.Debug("created handler proxy", "prefix", h.Prefix, "target", targetURL.String())
	}

	c.logger.Info("CreateHandlers returned handlers", "count", len(handlers))
	return handlers, nil
}

// Connected implements chain.ChainVM
func (c *Client) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	req := &zapwire.ConnectedRequest{
		NodeID: nodeID[:],
	}
	if nodeVersion != nil {
		req.Name = nodeVersion.Name
		req.Major = uint32(nodeVersion.Major)
		req.Minor = uint32(nodeVersion.Minor)
		req.Patch = uint32(nodeVersion.Patch)
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := c.conn.Call(ctx, zapwire.MsgConnected, buf.Bytes())
	return err
}

// Disconnected implements chain.ChainVM
func (c *Client) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	req := &zapwire.DisconnectedRequest{
		NodeID: nodeID[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := c.conn.Call(ctx, zapwire.MsgDisconnected, buf.Bytes())
	return err
}

// HealthCheck implements chain.ChainVM
func (c *Client) HealthCheck(ctx context.Context) (block.HealthCheckResult, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgHealth, nil)
	if err != nil {
		return block.HealthCheckResult{}, fmt.Errorf("health check failed: %w", err)
	}

	resp := &zapwire.HealthResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return block.HealthCheckResult{}, err
	}

	result := block.HealthCheckResult{
		Healthy: true,
	}

	// Parse details if present
	if len(resp.Details) > 0 {
		var details map[string]string
		if err := json.Unmarshal(resp.Details, &details); err == nil {
			result.Details = details
		}
	}

	return result, nil
}

// GetBlockIDAtHeight implements chain.ChainVM
func (c *Client) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	req := &zapwire.GetBlockIDAtHeightRequest{
		Height: height,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, respData, err := c.conn.Call(ctx, zapwire.MsgGetBlockIDAtHeight, buf.Bytes())
	if err != nil {
		return ids.Empty, err
	}

	resp := &zapwire.GetBlockIDAtHeightResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return ids.Empty, err
	}

	if resp.Err != zapwire.ErrorUnspecified {
		return ids.Empty, errorFromZAP(resp.Err)
	}

	var blkID ids.ID
	copy(blkID[:], resp.BlkID)
	return blkID, nil
}

// WaitForEvent implements chain.ChainVM
func (c *Client) WaitForEvent(ctx context.Context) (block.Message, error) {
	_, respData, err := c.conn.Call(ctx, zapwire.MsgWaitForEvent, nil)
	if err != nil {
		return block.Message{}, err
	}

	resp := &zapwire.WaitForEventResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return block.Message{}, err
	}

	return block.Message{
		Type: block.MessageType(resp.Message),
	}, nil
}

// Close closes the connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// zapBlock implements block.Block
type zapBlock struct {
	client    *Client
	id        ids.ID
	parentID  ids.ID
	bytes     []byte
	height    uint64
	timestamp time.Time
	status    uint8
}

func (b *zapBlock) ID() ids.ID           { return b.id }
func (b *zapBlock) Parent() ids.ID       { return b.parentID }
func (b *zapBlock) ParentID() ids.ID     { return b.parentID }
func (b *zapBlock) Bytes() []byte        { return b.bytes }
func (b *zapBlock) Height() uint64       { return b.height }
func (b *zapBlock) Timestamp() time.Time { return b.timestamp }
func (b *zapBlock) Status() uint8        { return b.status }

func (b *zapBlock) Verify(ctx context.Context) error {
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

func (b *zapBlock) Accept(ctx context.Context) error {
	req := &zapwire.BlockAcceptRequest{
		ID: b.id[:],
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	_, _, err := b.client.conn.Call(ctx, zapwire.MsgBlockAccept, buf.Bytes())
	return err
}

func (b *zapBlock) Reject(ctx context.Context) error {
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

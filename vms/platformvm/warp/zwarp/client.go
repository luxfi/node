// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zwarp provides ZAP transport for warp messaging.
package zwarp

import (
	"context"
	"errors"
	"fmt"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/node/vms/platformvm/warp"
)

var (
	_ warp.Signer = (*Client)(nil)

	ErrSigningFailed = errors.New("zwarp: signing failed")
)

// Client implements warp.Signer over ZAP transport
type Client struct {
	conn *zapwire.Conn
}

// NewClient creates a new ZAP-based warp signer client
func NewClient(conn *zapwire.Conn) *Client {
	return &Client{conn: conn}
}

// Sign implements warp.Signer
func (c *Client) Sign(unsignedMsg *warp.UnsignedMessage) ([]byte, error) {
	req := &zapwire.WarpSignRequest{
		NetworkID:     unsignedMsg.NetworkID,
		SourceChainID: unsignedMsg.SourceChainID[:],
		Payload:       unsignedMsg.Payload,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	respType, respData, err := c.conn.Call(context.Background(), zapwire.MsgWarpSign, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("zwarp sign: %w", err)
	}

	if respType&^zapwire.MsgResponseFlag != zapwire.MsgWarpSign {
		return nil, fmt.Errorf("zwarp: unexpected response type %d", respType)
	}

	resp := &zapwire.WarpSignResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return nil, fmt.Errorf("zwarp decode response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("%w: %s", ErrSigningFailed, resp.Error)
	}

	return resp.Signature, nil
}

// BatchSign signs multiple messages in a single call (optimization for HFT)
func (c *Client) BatchSign(messages []*warp.UnsignedMessage) ([][]byte, []error) {
	req := &zapwire.WarpBatchSignRequest{
		Messages: make([]zapwire.WarpSignRequest, len(messages)),
	}

	for i, msg := range messages {
		req.Messages[i] = zapwire.WarpSignRequest{
			NetworkID:     msg.NetworkID,
			SourceChainID: msg.SourceChainID[:],
			Payload:       msg.Payload,
		}
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	respType, respData, err := c.conn.Call(context.Background(), zapwire.MsgWarpBatchSign, buf.Bytes())
	if err != nil {
		errs := make([]error, len(messages))
		for i := range errs {
			errs[i] = err
		}
		return nil, errs
	}

	if respType&^zapwire.MsgResponseFlag != zapwire.MsgWarpBatchSign {
		errs := make([]error, len(messages))
		for i := range errs {
			errs[i] = fmt.Errorf("zwarp: unexpected response type %d", respType)
		}
		return nil, errs
	}

	resp := &zapwire.WarpBatchSignResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		errs := make([]error, len(messages))
		for i := range errs {
			errs[i] = fmt.Errorf("zwarp decode batch response: %w", err)
		}
		return nil, errs
	}

	// Convert errors
	errs := make([]error, len(resp.Errors))
	for i, errStr := range resp.Errors {
		if errStr != "" {
			errs[i] = fmt.Errorf("%w: %s", ErrSigningFailed, errStr)
		}
	}

	return resp.Signatures, errs
}

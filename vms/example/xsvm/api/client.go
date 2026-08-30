// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"bytes"
	"cmp"
	"context"
	stdjson "encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/zap-proto/zip"

	"github.com/luxfi/ids"
	node "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/vms/example/xsvm/block"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/example/xsvm/tx"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/rpc"
)

const DefaultPollingInterval = 50 * time.Millisecond

// This chain answers typed ops under its own base, so a call is a URL and a
// reply is that op's Out. There is no method name in a body: the address IS the
// operation. [zip.Query] turns the op's own In into the query string, so how an
// id is written here is derived exactly as it is read there.
type Client struct {
	// ops is where this chain serves its typed surface.
	ops string
}

func NewClient(uri, chain string) *Client {
	return &Client{ops: node.Chain(uri, chain) + node.Ops}
}

// ask reads one op. in is the op's own In; its fields become the query.
func (c *Client) ask(ctx context.Context, path string, in any, out any, options ...rpc.Option) error {
	query, err := zip.Query(in)
	if err != nil {
		return err
	}
	at, err := url.Parse(c.ops + path)
	if err != nil {
		return err
	}
	opts := rpc.NewOptions(options)
	for name, values := range opts.QueryParams() {
		for _, value := range values {
			query = cmp.Or(query+"&", "") + url.QueryEscape(name) + "=" + url.QueryEscape(value)
		}
	}
	at.RawQuery = query

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, at.String(), nil)
	if err != nil {
		return err
	}
	req.Header = opts.Headers()
	return c.do(req, out)
}

// send issues one write. The body is the op's own In, as JSON.
func (c *Client) send(ctx context.Context, path string, in any, out any, options ...rpc.Option) error {
	body, err := stdjson.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ops+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header = rpc.NewOptions(options).Headers()
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

// do runs one request and decodes its reply. A refusal carries the op's own
// message, which is what a caller needs to tell "no such block" from "not
// allowed".
func (c *Client) do(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %s: %s", req.Method, req.URL, resp.Status, said)
	}
	return stdjson.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Network(
	ctx context.Context,
	options ...rpc.Option,
) (uint32, ids.ID, ids.ID, error) {
	resp := new(NetworkReply)
	err := c.ask(ctx, "/network", struct{}{}, resp, options...)
	return resp.NetworkID, resp.ChainID, resp.ChainID, err
}

func (c *Client) Genesis(
	ctx context.Context,
	options ...rpc.Option,
) (*genesis.Genesis, error) {
	resp := new(GenesisReply)
	err := c.ask(ctx, "/genesis", struct{}{}, resp, options...)
	return resp.Genesis, err
}

func (c *Client) Nonce(
	ctx context.Context,
	address ids.ShortID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(NonceReply)
	err := c.ask(ctx, "/nonce", &NonceArgs{
		Address: address,
	}, resp, options...)
	return resp.Nonce, err
}

func (c *Client) Balance(
	ctx context.Context,
	address ids.ShortID,
	assetID ids.ID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(BalanceReply)
	err := c.ask(ctx, "/balance", &BalanceArgs{
		Address: address,
		AssetID: assetID,
	}, resp, options...)
	return resp.Balance, err
}

func (c *Client) Loan(
	ctx context.Context,
	chainID ids.ID,
	options ...rpc.Option,
) (uint64, error) {
	resp := new(LoanReply)
	err := c.ask(ctx, "/loan", &LoanArgs{
		ChainID: chainID,
	}, resp, options...)
	return resp.Amount, err
}

func (c *Client) IssueTx(
	ctx context.Context,
	newTx *tx.Tx,
	options ...rpc.Option,
) (ids.ID, error) {
	txBytes, err := newTx.Marshal()
	if err != nil {
		return ids.Empty, err
	}

	resp := new(IssueTxReply)
	err = c.send(ctx, node.Relay, &IssueTxArgs{
		Tx: txBytes,
	}, resp, options...)
	return resp.TxID, err
}

func (c *Client) LastAccepted(
	ctx context.Context,
	options ...rpc.Option,
) (ids.ID, *block.Stateless, error) {
	resp := new(LastAcceptedReply)
	err := c.ask(ctx, "/block/last", struct{}{}, resp, options...)
	return resp.BlockID, resp.Block, err
}

func (c *Client) Block(
	ctx context.Context,
	blkID ids.ID,
	options ...rpc.Option,
) (*block.Stateless, error) {
	resp := new(BlockReply)
	err := c.ask(ctx, "/block", &BlockArgs{
		BlockID: blkID,
	}, resp, options...)
	return resp.Block, err
}

func (c *Client) Message(
	ctx context.Context,
	txID ids.ID,
	options ...rpc.Option,
) (*warp.UnsignedMessage, []byte, error) {
	resp := new(MessageReply)
	err := c.ask(ctx, "/message", &MessageArgs{
		TxID: txID,
	}, resp, options...)
	if err != nil {
		return nil, nil, err
	}
	return resp.Message, resp.Signature, resp.Message.Initialize()
}

func AwaitTxAccepted(
	ctx context.Context,
	c *Client,
	address ids.ShortID,
	nonce uint64,
	freq time.Duration,
	options ...rpc.Option,
) error {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		currentNonce, err := c.Nonce(ctx, address, options...)
		if err != nil {
			return err
		}

		if currentNonce > nonce {
			// The nonce increasing indicates the acceptance of a transaction
			// issued with the specified nonce.
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

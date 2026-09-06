// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// The wire to a running node. Three implementations serve the C-chain's
// external Ethereum JSON-RPC on three different paths, and that difference is
// the only thing this file knows about them; every method below is the same
// method on all three, because the point of the differential is that the same
// request goes to each.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/luxfi/geth/common"
)

// Node is one running implementation, named by the language that wrote it.
type Node struct {
	Lang    string // go, cpp, rust
	URL     string
	ChainID *big.Int
	http    *http.Client
}

// NewNode dials nothing; the first call proves the node is there.
func NewNode(lang, url string) *Node {
	return &Node{Lang: lang, URL: url,
		http: &http.Client{Timeout: 20 * time.Second}}
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcErr) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcErr         `json:"error"`
}

// Call makes one JSON-RPC call and returns the raw result. An RPC-level error
// is returned as *rpcErr so a caller can tell "the node said no" from "the node
// is not there", which is exactly the distinction a differential reports on.
func (n *Node) Call(method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", n.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out rpcResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: not json-rpc: %.120s", method, raw)
	}
	if out.Error != nil {
		return nil, out.Error
	}
	return out.Result, nil
}

func (n *Node) str(method string, params ...any) (string, error) {
	raw, err := n.Call(method, params...)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%s: %w", method, err)
	}
	return s, nil
}

// Quantity reads a 0x-prefixed hex number, which is how every numeric answer
// on this wire is spelled.
func (n *Node) Quantity(method string, params ...any) (*big.Int, error) {
	s, err := n.str(method, params...)
	if err != nil {
		return nil, err
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("%s: not a quantity: %q", method, s)
	}
	return v, nil
}

func (n *Node) BlockNumber() (*big.Int, error) { return n.Quantity("eth_blockNumber") }

func (n *Node) Balance(a common.Address) (*big.Int, error) {
	return n.Quantity("eth_getBalance", a, "latest")
}

// Nonce is the ACCEPTED nonce, never the pending one. The distinction is the
// whole difference between a chain that is working and a chain that is only
// taking submissions: a node stuck in bootstrap still admits transactions to
// its pool and still counts them in its pending nonce, so a harness that asked
// for "pending" would watch that number climb and call a chain live that has
// accepted nothing. Every nonce this harness needs to send with, it tracks
// itself, so nothing is lost by refusing to ask.
func (n *Node) Nonce(a common.Address) (uint64, error) {
	v, err := n.Quantity("eth_getTransactionCount", a, "latest")
	if err != nil {
		return 0, err
	}
	return v.Uint64(), nil
}

func (n *Node) Code(a common.Address) ([]byte, error) {
	s, err := n.str("eth_getCode", a, "latest")
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

// StorageAt reads one slot. This is the primitive the state comparison is
// built on: a pool's reserves and a book's orders are storage, and storage is
// the same shape in every implementation because the EVM defines it.
func (n *Node) StorageAt(a common.Address, slot common.Hash) (common.Hash, error) {
	s, err := n.str("eth_getStorageAt", a, slot, "latest")
	if err != nil {
		return common.Hash{}, err
	}
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(b), nil
}

// CallResult is an eth_call: what the chain says a view function returns
// without changing anything.
func (n *Node) CallResult(to common.Address, data []byte) ([]byte, error) {
	s, err := n.str("eth_call", map[string]any{
		"to": to, "data": "0x" + hex.EncodeToString(data),
	}, "latest")
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

// SendRaw submits a signed transaction and returns its hash.
func (n *Node) SendRaw(raw []byte) (common.Hash, error) {
	s, err := n.str("eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
	if err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(s), nil
}

// Receipt is the part of a receipt this harness reads. Status is the whole
// point: a transaction that was mined and reverted is not a transaction that
// worked, and a harness that only checked inclusion would call a reverted swap
// a success.
type Receipt struct {
	Status          string          `json:"status"`
	ContractAddress *common.Address `json:"contractAddress"`
	BlockNumber     string          `json:"blockNumber"`
	GasUsed         string          `json:"gasUsed"`
}

func (n *Node) Receipt(h common.Hash) (*Receipt, error) {
	raw, err := n.Call("eth_getTransactionReceipt", h)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var r Receipt
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Await polls for a receipt. A chain that cannot build a block never produces
// one, and that is reported as a timeout rather than retried forever, because
// "this implementation does not advance" is an answer the comparison needs.
func (n *Node) Await(h common.Hash, d time.Duration) (*Receipt, error) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r, err := n.Receipt(h)
		if err == nil && r != nil {
			return r, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("no receipt for %s within %s", h.Hex(), d)
}

// StateRoot reads the accepted head's state root. Two implementations running
// the same operations against different genesis allocations will not share it;
// what it is good for is the within-implementation claim that the state moved
// at all, and for reporting honestly that a cross-implementation root
// comparison is not the comparison anyone wants.
func (n *Node) StateRoot() (common.Hash, string, error) {
	raw, err := n.Call("eth_getBlockByNumber", "latest", false)
	if err != nil {
		return common.Hash{}, "", err
	}
	var b struct {
		StateRoot string `json:"stateRoot"`
		Number    string `json:"number"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return common.Hash{}, "", err
	}
	return common.HexToHash(b.StateRoot), b.Number, nil
}

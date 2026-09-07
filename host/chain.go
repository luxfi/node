// SPDX-License-Identifier: BSD-3-Clause-Eco

package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/luxfi/ids"
)

// Chain defines the common interface for all hosted VMs in the node.
type Chain interface {
	ID() ids.ID
	Alias() string
	Name() string
	Status() string
	Height() uint64
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Start(ctx context.Context) error
	Stop() error
}

// JSON-RPC wire containers
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func readRPC(r *http.Request) (*rpcRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// ─── Platform Chain (P-Chain) ────────────────────────────────────────────────

type PlatformChain struct {
	id         ids.ID
	height     atomic.Uint64
	validators []string
	nodeID     ids.NodeID
	mu         sync.RWMutex
}

func NewPlatformChain(nodeID ids.NodeID) *PlatformChain {
	p := &PlatformChain{
		id:         ids.FromStringOrPanic("P"),
		validators: []string{nodeID.String()},
		nodeID:     nodeID,
	}
	p.height.Store(1)
	return p
}

func (p *PlatformChain) ID() ids.ID     { return p.id }
func (p *PlatformChain) Alias() string  { return "P" }
func (p *PlatformChain) Name() string   { return "Platform Chain" }
func (p *PlatformChain) Status() string { return "online" }
func (p *PlatformChain) Height() uint64 { return p.height.Load() }
func (p *PlatformChain) Start(ctx context.Context) error { return nil }
func (p *PlatformChain) Stop() error                    { return nil }

func (p *PlatformChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := readRPC(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	switch req.Method {
	case "platform.getHeight":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"height": fmt.Sprintf("%d", p.height.Load())},
		})
	case "platform.getCurrentValidators":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"validators": []map[string]any{
					{
						"nodeID":    p.nodeID.String(),
						"weight":    "1000000000000",
						"connected": true,
					},
				},
			},
		})
	case "platform.getL1Validators":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"validators":    []any{},
				"feeRate":       "1000",
				"continuousFee": true,
			},
		})
	case "platform.issueTx":
		h := sha256.Sum256(req.Params)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"txID": hex.EncodeToString(h[:])},
		})
	default:
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)},
		})
	}
}

// ─── Exchange Chain (X-Chain) ────────────────────────────────────────────────

type XChain struct {
	id     ids.ID
	height atomic.Uint64
}

func NewXChain() *XChain {
	x := &XChain{
		id: ids.FromStringOrPanic("X"),
	}
	x.height.Store(1)
	return x
}

func (x *XChain) ID() ids.ID     { return x.id }
func (x *XChain) Alias() string  { return "X" }
func (x *XChain) Name() string   { return "Exchange Chain" }
func (x *XChain) Status() string { return "online" }
func (x *XChain) Height() uint64 { return x.height.Load() }
func (x *XChain) Start(ctx context.Context) error { return nil }
func (x *XChain) Stop() error                    { return nil }

func (x *XChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := readRPC(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	switch req.Method {
	case "avm.getAssetDescription":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"name":         "Lux",
				"symbol":       "LUX",
				"denomination": 9,
			},
		})
	case "avm.getBalance":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"balance": "10000000000000",
				"utxoIDs": []string{},
			},
		})
	case "avm.issueTx":
		h := sha256.Sum256(req.Params)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"txID": hex.EncodeToString(h[:])},
		})
	case "avm.getTxStatus":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"status": "Accepted"},
		})
	default:
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)},
		})
	}
}

// ─── Contract Chain (C-Chain / EVM) ──────────────────────────────────────────

type EVMChain struct {
	id         ids.ID
	height     atomic.Uint64
	chainID    uint64
	archiveURL *url.URL
	proxy      *httputil.ReverseProxy
}

func NewEVMChain(chainID uint64, archiveRPC string) *EVMChain {
	evm := &EVMChain{
		id:      ids.FromStringOrPanic("C"),
		chainID: chainID,
	}
	evm.height.Store(1)
	if archiveRPC != "" {
		if u, err := url.Parse(archiveRPC); err == nil {
			evm.archiveURL = u
			evm.proxy = httputil.NewSingleHostReverseProxy(u)
		}
	}
	return evm
}

func (c *EVMChain) ID() ids.ID     { return c.id }
func (c *EVMChain) Alias() string  { return "C" }
func (c *EVMChain) Name() string   { return "Contract Chain (EVM)" }
func (c *EVMChain) Status() string { return "online" }
func (c *EVMChain) Height() uint64 { return c.height.Load() }
func (c *EVMChain) Start(ctx context.Context) error { return nil }
func (c *EVMChain) Stop() error                    { return nil }

func (c *EVMChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		// Fallback to archive proxy if configured
		if c.proxy != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			c.proxy.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	switch req.Method {
	case "eth_chainId":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  fmt.Sprintf("0x%x", c.chainID),
		})
	case "net_version":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  fmt.Sprintf("%d", c.chainID),
		})
	case "web3_clientVersion":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  "luxd/v2.0.0-trilang",
		})
	case "eth_blockNumber":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  fmt.Sprintf("0x%x", c.height.Load()),
		})
	case "eth_gasPrice":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  "0x3b9aca00", // 1 gwei
		})
	case "eth_getBalance":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  "0x56bc75e2d63100000", // 100 LUX
		})
	case "eth_sendRawTransaction":
		h := sha256.Sum256(req.Params)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  fmt.Sprintf("0x%s", hex.EncodeToString(h[:])),
		})
	default:
		// If archive proxy is available, delegate historical/state calls
		if c.proxy != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			c.proxy.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not implemented", req.Method)},
		})
	}
}

// ─── Quantum Chain (Q-Chain) ─────────────────────────────────────────────────

type QuantumChain struct {
	id     ids.ID
	height atomic.Uint64
}

func NewQuantumChain() *QuantumChain {
	q := &QuantumChain{
		id: ids.FromStringOrPanic("Q"),
	}
	q.height.Store(1)
	return q
}

func (q *QuantumChain) ID() ids.ID     { return q.id }
func (q *QuantumChain) Alias() string  { return "Q" }
func (q *QuantumChain) Name() string   { return "Quantum Chain" }
func (q *QuantumChain) Status() string { return "online" }
func (q *QuantumChain) Height() uint64 { return q.height.Load() }
func (q *QuantumChain) Start(ctx context.Context) error { return nil }
func (q *QuantumChain) Stop() error                    { return nil }

func (q *QuantumChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := readRPC(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	switch req.Method {
	case "quantum.getHeight":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"height": q.height.Load()},
		})
	case "quantum.issueTx":
		h := sha256.Sum256(req.Params)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"txID": hex.EncodeToString(h[:])},
		})
	case "quantum.getBlock":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"height": q.height.Load(),
				"algorithm": "ML-DSA-65",
				"valid": true,
			},
		})
	default:
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)},
		})
	}
}

// ─── Shielded Chain (Z-Chain / ZkVM) ─────────────────────────────────────────

type ZKChain struct {
	id         ids.ID
	height     atomic.Uint64
	nullifiers sync.Map
}

func NewZKChain() *ZKChain {
	z := &ZKChain{
		id: ids.FromStringOrPanic("Z"),
	}
	z.height.Store(1)
	return z
}

func (z *ZKChain) ID() ids.ID     { return z.id }
func (z *ZKChain) Alias() string  { return "Z" }
func (z *ZKChain) Name() string   { return "Shielded Chain (ZKVM)" }
func (z *ZKChain) Status() string { return "online" }
func (z *ZKChain) Height() uint64 { return z.height.Load() }
func (z *ZKChain) Start(ctx context.Context) error { return nil }
func (z *ZKChain) Stop() error                    { return nil }

func (z *ZKChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := readRPC(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
		return
	}

	switch req.Method {
	case "zk.getHeight":
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"height": z.height.Load()},
		})
	case "zk.issueShieldedTx":
		h := sha256.Sum256(req.Params)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"txID": hex.EncodeToString(h[:])},
		})
	case "zk.getNullifierStatus":
		var params struct {
			Nullifier string `json:"nullifier"`
		}
		_ = json.Unmarshal(req.Params, &params)
		_, spent := z.nullifiers.Load(params.Nullifier)
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"spent": spent},
		})
	default:
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)},
		})
	}
}

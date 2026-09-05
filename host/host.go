// SPDX-License-Identifier: BSD-3-Clause-Eco

package host

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// Config configures the Go node host daemon.
type Config struct {
	DataDir      string
	RPCAddr      string
	StakeAddr    string
	NetworkName  string
	ArchiveRPC   string
	LightMode    bool
	ProduceDelay time.Duration
	GenesisFile  string
}

// DefaultConfig provides default settings for localnet.
func DefaultConfig() Config {
	return Config{
		DataDir:      ".lux",
		RPCAddr:      "127.0.0.1:9650",
		StakeAddr:    "127.0.0.1:9651",
		NetworkName:  "localnet",
		ProduceDelay: 200 * time.Millisecond,
	}
}

// Host is the unified daemon hosting all 5 chains (P, X, C, Q, Z).
type Host struct {
	cfg        Config
	identity   *Identity
	chains     map[string]Chain
	aliases    map[string]string
	httpServer *http.Server
	rpcLn      net.Listener
	stakeLn    net.Listener
	running    atomic.Bool
	stopCh     chan struct{}
	mu         sync.RWMutex
}

// New constructs a new node host.
func New(cfg Config) (*Host, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = ".lux"
	}
	if cfg.RPCAddr == "" {
		cfg.RPCAddr = "127.0.0.1:9650"
	}
	if cfg.StakeAddr == "" {
		cfg.StakeAddr = "127.0.0.1:9651"
	}
	if cfg.NetworkName == "" {
		cfg.NetworkName = "localnet"
	}

	id, err := EnsureIdentity(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ensure identity: %w", err)
	}

	chainID := uint64(200200) // localnet default
	if strings.EqualFold(cfg.NetworkName, "mainnet") {
		chainID = 96369
	}

	pChain := NewPlatformChain(id.NodeID)
	xChain := NewXChain()
	cChain := NewEVMChain(chainID, cfg.ArchiveRPC)
	qChain := NewQuantumChain()
	zChain := NewZKChain()

	chains := map[string]Chain{
		"P": pChain,
		"X": xChain,
		"C": cChain,
		"Q": qChain,
		"Z": zChain,
	}

	aliases := map[string]string{
		"p":       "P",
		"x":       "X",
		"c":       "C",
		"q":       "Q",
		"z":       "Z",
		"zoo":     "C",
		"hanzo":   "C",
		"primary": "P",
	}

	h := &Host{
		cfg:      cfg,
		identity: id,
		chains:   chains,
		aliases:  aliases,
		stopCh:   make(chan struct{}),
	}

	return h, nil
}

// NodeID returns this node's staking identity.
func (h *Host) NodeID() ids.NodeID {
	return h.identity.NodeID
}

// Chains returns the active chain set.
func (h *Host) Chains() map[string]Chain {
	return h.chains
}

// Start boots the host listeners and begins serving RPC.
func (h *Host) Start(ctx context.Context) error {
	if !h.running.CompareAndSwap(false, true) {
		return fmt.Errorf("host is already running")
	}

	// 1. Staking TLS listener
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{h.identity.TLS},
		MinVersion:   tls.VersionTLS13,
	}
	stakeLn, err := tls.Listen("tcp", h.cfg.StakeAddr, tlsConf)
	if err != nil {
		return fmt.Errorf("bind staking port: %w", err)
	}
	h.stakeLn = stakeLn

	// Accept staking connections in background
	go func() {
		for {
			conn, err := stakeLn.Accept()
			if err != nil {
				select {
				case <-h.stopCh:
					return
				default:
					continue
				}
			}
			// Connection established
			_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
			}(conn)
		}
	}()

	// 2. HTTP JSON-RPC router
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/info", h.handleInfo)
	mux.HandleFunc("/metrics", h.handleMetrics)
	mux.HandleFunc("/v1/chain/", h.handleChain)

	rpcLn, err := net.Listen("tcp", h.cfg.RPCAddr)
	if err != nil {
		_ = stakeLn.Close()
		return fmt.Errorf("bind rpc port: %w", err)
	}
	h.rpcLn = rpcLn

	h.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		_ = h.httpServer.Serve(rpcLn)
	}()

	return nil
}

// Stop halts the node host daemon cleanly.
func (h *Host) Stop() error {
	if !h.running.CompareAndSwap(true, false) {
		return nil
	}
	close(h.stopCh)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if h.httpServer != nil {
		_ = h.httpServer.Shutdown(ctx)
	}
	if h.stakeLn != nil {
		_ = h.stakeLn.Close()
	}
	return nil
}

// Handler for /health
func (h *Host) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":  "ok",
		"healthy": true,
		"time":    time.Now().UTC().Format(time.RFC3339),
		"chains": map[string]string{
			"P": "online",
			"X": "online",
			"C": "online",
			"Q": "online",
			"Z": "online",
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// Handler for /info
func (h *Host) handleInfo(w http.ResponseWriter, r *http.Request) {
	var pubKey string
	if pk := h.identity.PublicKey(); pk != nil {
		pubKey = hex.EncodeToString(bls.PublicKeyToCompressedBytes(pk))
	}
	resp := map[string]any{
		"version":   "luxd/v2.0.0-trilang",
		"network":   h.cfg.NetworkName,
		"nodeID":    h.identity.NodeID.String(),
		"publicKey": pubKey,
		"chains":    []string{"P", "X", "C", "Q", "Z"},
		"rpcAddr":   h.cfg.RPCAddr,
		"stakeAddr": h.cfg.StakeAddr,
	}
	writeJSON(w, http.StatusOK, resp)
}

// Handler for /metrics
func (h *Host) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "# HELP lux_node_up Node operational status\n# TYPE lux_node_up gauge\nlux_node_up 1\n")
	for name, c := range h.chains {
		fmt.Fprintf(w, "lux_chain_height{chain=\"%s\"} %d\n", name, c.Height())
	}
}

// Handler for / (root overview)
func (h *Host) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// If path has /v1/chain, let handleChain handle it
		if strings.HasPrefix(r.URL.Path, "/v1/chain") {
			h.handleChain(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	resp := map[string]any{
		"daemon":    "luxd",
		"version":   "v2.0.0-trilang",
		"nodeID":    h.identity.NodeID.String(),
		"network":   h.cfg.NetworkName,
		"endpoints": []string{
			"/v1/chain/p", "/v1/chain/p/rpc",
			"/v1/chain/x", "/v1/chain/x/rpc",
			"/v1/chain/c", "/v1/chain/c/rpc",
			"/v1/chain/q", "/v1/chain/q/rpc",
			"/v1/chain/z", "/v1/chain/z/rpc",
			"/health", "/info", "/metrics",
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// Handler for /v1/chain/{alias} and /v1/chain/{alias}/rpc
func (h *Host) handleChain(w http.ResponseWriter, r *http.Request) {
	// Trim /v1/chain/
	path := strings.TrimPrefix(r.URL.Path, "/v1/chain/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	target := parts[0]
	// Lookup alias
	canonical, ok := h.aliases[strings.ToLower(target)]
	if !ok {
		canonical = strings.ToUpper(target)
	}

	chain, ok := h.chains[canonical]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": fmt.Sprintf("chain '%s' not found", target),
		})
		return
	}

	chain.ServeHTTP(w, r)
}

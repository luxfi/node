// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap provides ZAP transport implementation for VM RPC.
package zap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	rpcdbzap "github.com/luxfi/node/db/rpcdb"
	"github.com/luxfi/version"
	"github.com/luxfi/vm/chain"
	atomicmem "github.com/luxfi/vm/chains/atomic"
	"github.com/luxfi/validators"
	"github.com/luxfi/vm/chains/atomic/atomiczap"
	"github.com/luxfi/vm/validatorzap"
)

var (
	ErrNotConnected    = errors.New("zap: not connected")
	ErrInvalidResponse = errors.New("zap: invalid response")
	// errMalformedBlockID is returned when the VM plugin sends a block-id field
	// over the ZAP boundary whose length is not exactly ids.IDLen (32 bytes).
	// An empty (0-length) field is the plugin's way of signalling that it could
	// NOT resolve the block — e.g. the requested block does not connect to this
	// node's accepted chain (a fork / missing-parent condition). We surface that
	// as this explicit typed error instead of the opaque "invalid hash length"
	// from ids.ToID, and we NEVER coerce it to ids.Empty (a 32-byte zero id is a
	// legitimate value; a 0-length field is not).
	errMalformedBlockID = errors.New("zap: plugin returned malformed block id (does not connect to accepted chain)")
)

// Compile-time check that Client implements chain.ChainVM
var _ chain.ChainVM = (*Client)(nil)

// Client implements chain.ChainVM over ZAP transport
type Client struct {
	conn   *zapwire.Conn
	logger log.Logger

	// lastAcceptedID caches the plugin's last-accepted block id. SEEDED at Initialize and
	// REFRESHED on every successful block Accept (setLastAccepted) so LastAccepted() honors the
	// block.ChainVM contract — return the ACTUAL last-accepted, not a frozen Initialize snapshot.
	// Before this refresh the cache froze for the process life: a fire-and-forget Accept advanced
	// the plugin's on-disk store but never the cache, so GetAcceptedFrontier served a stale tip and
	// any consumer reading VM.LastAccepted was misled. Guarded by lastAcceptedMu because Accept (the
	// consensus accept goroutine) and LastAccepted (the network/bootstrap goroutines) race.
	lastAcceptedMu sync.RWMutex
	lastAcceptedID ids.ID

	// dbServer is the ZAP-native rpcdb server spawned in Initialize that
	// hosts init.DB for the plugin's outbound RemoteZapDB to dial. It is
	// the ZAP equivalent of the gRPC `dbServerListener` pattern in
	// vm/rpc/vm_client.go:201. Lifetime is bound to this Client.
	dbServerMu  sync.Mutex
	dbServer    *rpcdbzap.ZAPServer
	dbListener  net.Listener
	dbServerCtx context.Context
	dbServerCxl context.CancelFunc

	// atomicServer hosts this chain's atomic shared-memory handle for the plugin
	// to dial, exactly as dbServer hosts init.DB. It exists because the handle
	// is an interface over a live database and so cannot be copied into the
	// Runtime the plugin rebuilds from InitializeRequest — the reason every
	// plugin-hosted VM saw a nil SharedMemory. Lifetime bound to this Client.
	atomicMu   sync.Mutex
	atomicStop func()

	validatorMu   sync.Mutex
	validatorStop func()

	// quasarCapable records whether the plugin advertised CapQuasarExport in the
	// Initialize handshake — i.e. its concrete VM tracks a Quasar (⅔-by-stake)
	// EXPORT-FINAL height across the process boundary. Written ONCE at Initialize
	// (before the chain manager wires the consensus observer or reads the export
	// height), read by SetLastQuasarFinalized/LastQuasarHeight/SupportsQuasarExport.
	// atomic so the -race detector is satisfied without pretending the callers
	// share a lock. A plugin that does not advertise it → false → the export
	// methods are graceful no-ops and the chain manager stays Nova-only.
	quasarCapable atomic.Bool

	// syncCapable records whether the plugin advertised CapStateSync at the
	// handshake. Written once at Initialize, read before any state-sync message
	// is sent. Clear ⇒ the surface is absent and the node bootstraps that chain
	// block by block, which is what it did before the surface existed.
	syncCapable atomic.Bool
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
	var xChainID, cChainID, utxoAssetID []byte
	var chainDataDir string

	if init.Runtime != nil {
		rt := init.Runtime
		networkID = rt.NetworkID
		chainID = rt.ChainID[:]
		nodeID = rt.NodeID[:]
		publicKey = rt.PublicKey
		xChainID = rt.XChainID[:]
		cChainID = rt.CChainID[:]
		utxoAssetID = rt.UTXOAssetID[:]
		chainDataDir = rt.ChainDataDir
	}

	// Spawn the ZAP rpcdb server before calling Initialize so the VM can
	// dial it back as part of its own bootstrap. Mirrors the gRPC
	// pattern in vm/rpc/vm_client.go:201 but speaks ZAP. The listener
	// lifetime is bound to this Client (closed by Shutdown).
	dbServerAddr, err := c.startDBServer(init.DB)
	if err != nil {
		return fmt.Errorf("zap: start db server: %w", err)
	}

	// CROSS-CHAIN ATOMIC CAPABILITY. Serve this chain's shared-memory handle and
	// resolve the one chain alias DEX settlement needs, because neither the handle
	// nor BCLookup can be copied into the Runtime the plugin rebuilds on the far
	// side. Without these the plugin's SharedMemory is nil and DChainID is empty,
	// so every settlement reverts.
	var (
		atomicServerAddr    string
		validatorServerAddr string
		dChainID            ids.ID
	)
	if init.Runtime != nil {
		if atomicServerAddr, err = c.startAtomicServer(init.Runtime.SharedMemory); err != nil {
			return fmt.Errorf("zap: start atomic server: %w", err)
		}
		// VALIDATOR STATE, same seam and same reason: an interface over live
		// node-owned state that the plugin cannot receive by value. Without it a
		// plugin VM sees a nil handle and can form no committee — which is why
		// M-Chain, whose whole job is a threshold ceremony among validators,
		// could never run one.
		if validatorServerAddr, err = c.startValidatorServer(init.Runtime.ValidatorState); err != nil {
			return fmt.Errorf("zap: start validator server: %w", err)
		}
		// BCLookup is the node's chain manager. Resolve "D" here, once, rather
		// than proxy a lookup service: exactly this one alias is needed and its
		// value is fixed for the life of the network. A network with no dexvm
		// leaves it empty and the path stays honestly closed.
		if bc := init.Runtime.GetBCLookup(); bc != nil {
			if id, lerr := bc.Lookup("D"); lerr == nil {
				dChainID = id
			}
		}
	}

	req := &zapwire.InitializeRequest{
		NetworkID:    networkID,
		ChainID:      chainID,
		NodeID:       nodeID,
		PublicKey:    publicKey,
		XChainID:     xChainID,
		CChainID:     cChainID,
		UTXOAssetID:  utxoAssetID,
		ChainDataDir: chainDataDir,
		GenesisBytes: init.Genesis,
		UpgradeBytes: init.Upgrade,
		ConfigBytes:  init.Config,
		DBServerAddr: dbServerAddr,

		AtomicServerAddr:    atomicServerAddr,
		DChainID:            dChainIDBytes(dChainID),
		ValidatorServerAddr: validatorServerAddr,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req.Encode(buf)

	respType, respData, err := c.conn.Call(ctx, zapwire.MsgInitialize, buf.Bytes())
	if err != nil {
		return fmt.Errorf("zap initialize: %w", err)
	}

	// Strip both MsgResponseFlag (always set on responses) and MsgErrorFlag
	// (set on error responses, though the transport's readLoop already
	// converts those into a non-nil err above) before comparing to the
	// request message type. Conflating MsgResponseFlag with "error" — as
	// earlier code did — corrupted every successful Initialize because the
	// binary InitializeResponse payload was rendered as an error string,
	// hiding the response and crashing chain creation with an unreadable
	// "vm error: <random-looking bytes>" message.
	if respType&^(zapwire.MsgResponseFlag|zapwire.MsgErrorFlag) != zapwire.MsgInitialize {
		return ErrInvalidResponse
	}

	resp := &zapwire.InitializeResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		return fmt.Errorf("zap decode initialize response: %w", err)
	}

	seedID, err := blockIDFromZAP("lastAcceptedID", resp.LastAcceptedID)
	if err != nil {
		return err
	}
	c.setLastAccepted(seedID)

	// Capture the plugin's OPTIONAL export capability from the handshake. Only a
	// VM that advertises CapQuasarExport participates in the two-tier consensus
	// export frontier across this boundary; everything else stays Nova-only.
	c.quasarCapable.Store(resp.Capabilities&zapwire.CapQuasarExport != 0)
	// And whether it answers the state-sync messages at all. A plugin built
	// before they existed hits its own unknown-message arm and replies with
	// nothing, which reaches a decoder as a truncated frame rather than as an
	// answer — so the capability is what separates "this plugin does not do
	// that" from "the boundary is broken", and only one of those is worth
	// reporting as a fault.
	c.syncCapable.Store(resp.Capabilities&zapwire.CapStateSync != 0)

	c.logger.Info("VM initialized via ZAP",
		"height", resp.Height,
		"lastAcceptedID", seedID,
		"quasarExport", c.quasarCapable.Load(),
		"stateSync", c.syncCapable.Load(),
	)

	return nil
}

// setLastAccepted refreshes the cached last-accepted id under the write lock. Called at
// Initialize (seed) and on every successful zapBlock.Accept so the cache tracks the plugin's
// real accepted tip instead of freezing at the boot snapshot.
func (c *Client) setLastAccepted(id ids.ID) {
	c.lastAcceptedMu.Lock()
	c.lastAcceptedID = id
	c.lastAcceptedMu.Unlock()
}

// Shutdown implements chain.ChainVM
func (c *Client) Shutdown(ctx context.Context) error {
	_, _, err := c.conn.Call(ctx, zapwire.MsgShutdown, nil)
	if err != nil {
		c.logger.Warn("ZAP shutdown error", "error", err)
	}
	c.stopDBServer()
	c.stopAtomicServer()
	c.stopValidatorServer()
	return c.conn.Close()
}

// dChainIDBytes renders an optional chain id for the wire: an unresolved alias
// travels as a ZERO-LENGTH field, never as 32 zero bytes. ids.Empty is a
// legitimate 32-byte value, so encoding "absent" as those bytes would be
// indistinguishable from a real id and the plugin could not tell "no dexvm on
// this network" from "the dexvm is the zero chain".
func dChainIDBytes(id ids.ID) []byte {
	if id == ids.Empty {
		return nil
	}
	return id[:]
}

// startDBServer binds a fresh TCP listener and serves init.DB over the
// ZAP rpcdb wire protocol against it. Returns the bound addr to pass
// to the plugin via InitializeRequest.DBServerAddr. The listener and
// server are tracked on the Client so Shutdown can close them
// deterministically.
//
// Returns "" if init.DB is nil — some VMs (the platform VM bootstrap)
// init the DB elsewhere; in that case the plugin falls back to its
// LocalZapDB path. Production cevm always gets a non-nil DB.
// startAtomicServer binds a fresh listener and serves the chain's atomic
// shared-memory handle over ZAP against it, returning the bound addr to hand the
// plugin via InitializeRequest.AtomicServerAddr. Same shape and lifetime as
// startDBServer.
//
// Returns "" when sm is nil — the node wired no shared memory for this chain, so
// the plugin must leave its handle nil and let settlement revert fail-closed.
// Never fabricate a server over a nil handle: that would turn a clean "capability
// absent" revert into a nil dereference inside block execution.
func (c *Client) startAtomicServer(sm atomicmem.SharedMemory) (string, error) {
	addr, stop, err := atomiczap.Serve(sm)
	if err != nil {
		return "", err
	}
	if addr == "" {
		c.logger.Warn("zap: no shared memory for this chain — the cross-chain atomic path stays closed")
		return "", nil
	}

	c.atomicMu.Lock()
	c.atomicStop = stop
	c.atomicMu.Unlock()

	c.logger.Info("zap atomic shared-memory server listening", "addr", addr)
	return addr, nil
}

// startValidatorServer binds a fresh listener and serves the node's validator
// state over ZAP, returning the bound addr to hand the plugin via
// InitializeRequest.ValidatorServerAddr. Same shape and lifetime as
// startAtomicServer.
//
// Returns "" when vs is nil — the node wired no validator state for this chain,
// so the plugin must leave its handle nil and let a committee lookup fail
// loudly. Never fabricate a server over a nil handle: an empty validator set is
// a quorum of nobody, and a ceremony that ran against one would be a ceremony
// among no one.
func (c *Client) startValidatorServer(vs validators.State) (string, error) {
	addr, stop, err := validatorzap.Serve(vs)
	if err != nil {
		return "", err
	}
	if addr == "" {
		c.logger.Warn("zap: no validator state for this chain — a plugin VM here can form no committee")
		return "", nil
	}

	c.validatorMu.Lock()
	c.validatorStop = stop
	c.validatorMu.Unlock()

	c.logger.Info("zap validator-state server listening", "addr", addr)
	return addr, nil
}

func (c *Client) stopValidatorServer() {
	c.validatorMu.Lock()
	defer c.validatorMu.Unlock()
	if c.validatorStop != nil {
		c.validatorStop()
		c.validatorStop = nil
	}
}

func (c *Client) stopAtomicServer() {
	c.atomicMu.Lock()
	defer c.atomicMu.Unlock()
	if c.atomicStop != nil {
		c.atomicStop()
		c.atomicStop = nil
	}
}

func (c *Client) startDBServer(db database.Database) (string, error) {
	if db == nil {
		c.logger.Warn("zap: init.DB is nil — VM will fall back to local backend")
		return "", nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("zap db listen: %w", err)
	}

	s := rpcdbzap.NewZAPServer(db)
	s.ListenOn(listener)

	c.dbServerMu.Lock()
	defer c.dbServerMu.Unlock()
	c.dbServer = s
	c.dbListener = listener
	c.dbServerCtx, c.dbServerCxl = context.WithCancel(context.Background())

	addr := listener.Addr().String()
	go func(ctx context.Context) {
		if err := s.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
			c.logger.Warn("zap db serve exited", "error", err)
		}
	}(c.dbServerCtx)

	c.logger.Info("zap db server listening", "addr", addr)
	return addr, nil
}

func (c *Client) stopDBServer() {
	c.dbServerMu.Lock()
	defer c.dbServerMu.Unlock()
	// Cancel ctx FIRST so Serve's loop exits; then closing the listener
	// makes any pending Accept() return an error promptly. Close()
	// itself is a no-op on the zapwire.Server map (we deliberately
	// don't touch it — see ZAPServer.Close comment).
	if c.dbServerCxl != nil {
		c.dbServerCxl()
		c.dbServerCxl = nil
	}
	if c.dbServer != nil {
		_ = c.dbServer.Close()
		c.dbServer = nil
	}
	c.dbListener = nil
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

// blockIDFromZAP converts a block-id field returned by the VM plugin over the
// ZAP boundary into an ids.ID, guarding the malformed/empty case. A well-formed
// plugin always returns exactly ids.IDLen bytes; any other length (notably the
// 0-length "could not resolve" signal) yields errMalformedBlockID rather than
// the opaque ids.ToID "invalid hash length" error, and never a coerced zero id.
func blockIDFromZAP(field string, b []byte) (ids.ID, error) {
	if len(b) != ids.IDLen {
		return ids.Empty, fmt.Errorf("%w: %s was %d bytes, want %d", errMalformedBlockID, field, len(b), ids.IDLen)
	}
	return ids.ToID(b)
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

	id, err := blockIDFromZAP("id", resp.ID)
	if err != nil {
		return nil, err
	}
	parentID, err := blockIDFromZAP("parentID", resp.ParentID)
	if err != nil {
		return nil, err
	}

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

	id, err := blockIDFromZAP("id", resp.ID)
	if err != nil {
		return nil, err
	}
	parentID, err := blockIDFromZAP("parentID", resp.ParentID)
	if err != nil {
		return nil, err
	}

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

	parentID, err := blockIDFromZAP("parentID", resp.ParentID)
	if err != nil {
		return nil, err
	}

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

// LastAccepted implements chain.ChainVM. Returns the cache refreshed on every Accept (NOT a
// frozen Initialize snapshot) under the read lock.
func (c *Client) LastAccepted(ctx context.Context) (ids.ID, error) {
	c.lastAcceptedMu.RLock()
	defer c.lastAcceptedMu.RUnlock()
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

	// Parse details if present. Wire shape (ZAP-native, k/v list):
	//
	//   [u32 count]( [u32 keylen][key bytes] [u32 vallen][val bytes] )*
	//
	// Servers that still emit legacy JSON details are tolerated: parse
	// failure is non-fatal and Details remains nil. The lux/vm server
	// emits an empty Details by default; the day-1 contract is "Details
	// is optional and opaque" so this is forward-compatible.
	if details, ok := decodeHealthDetails(resp.Details); ok {
		result.Details = details
	}

	return result, nil
}

// decodeHealthDetails parses a ZAP-encoded health-details k/v list. The
// envelope is described above HealthCheck; returns ok=false (silently)
// on any wire-shape mismatch so legacy JSON-emitting servers don't
// trigger spurious health failures.
func decodeHealthDetails(b []byte) (map[string]string, bool) {
	if len(b) < 4 {
		return nil, false
	}
	count := binary.LittleEndian.Uint32(b[:4])
	out := make(map[string]string, count)
	rest := b[4:]
	for i := uint32(0); i < count; i++ {
		if len(rest) < 4 {
			return nil, false
		}
		klen := binary.LittleEndian.Uint32(rest[:4])
		rest = rest[4:]
		if uint32(len(rest)) < klen {
			return nil, false
		}
		key := string(rest[:klen])
		rest = rest[klen:]
		if len(rest) < 4 {
			return nil, false
		}
		vlen := binary.LittleEndian.Uint32(rest[:4])
		rest = rest[4:]
		if uint32(len(rest)) < vlen {
			return nil, false
		}
		val := string(rest[:vlen])
		rest = rest[vlen:]
		out[key] = val
	}
	if len(rest) != 0 {
		return nil, false
	}
	return out, true
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

	blkID, err := blockIDFromZAP("blkID", resp.BlkID)
	if err != nil {
		return ids.Empty, err
	}
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

// quasarCallTimeout bounds a single Quasar export RPC. The export bridge fires
// off the consensus accept path (post-accept, not the hot vote path), so a short
// bound keeps a wedged plugin from stalling the observer without being so tight
// it drops a legitimate slow round-trip.
const quasarCallTimeout = 5 * time.Second

// SupportsQuasarExport reports whether the plugin advertised the Quasar
// (⅔-by-stake) EXPORT capability at Initialize. The chain manager gates the
// consensus export-frontier observer on this: false → the plugin is a generic VM
// with no export surface and the chain runs Nova-only (no cross-process no-op
// spam per finalization).
func (c *Client) SupportsQuasarExport() bool { return c.quasarCapable.Load() }

// SetLastQuasarFinalized forwards a new Quasar (⅔-by-stake) EXPORT-FINAL height
// to the plugin VM across the ZAP boundary so the plugin's `finalized`/`safe`
// tags and warp export gate track ⅔-stake finality, not the reorgable Nova tip.
// No-op (no RPC) if the plugin did not advertise CapQuasarExport — so the chain
// manager can wire this unconditionally. Fire-and-forget: a transport error is
// logged, not returned, because the sole caller is the consensus export-frontier
// observer, which has no error channel (the durable height is re-seeded on boot).
func (c *Client) SetLastQuasarFinalized(height uint64) {
	if !c.quasarCapable.Load() {
		return
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	req := &zapwire.SetQuasarFinalizedRequest{Height: height}
	req.Encode(buf)

	ctx, cancel := context.WithTimeout(context.Background(), quasarCallTimeout)
	defer cancel()
	if _, _, err := c.conn.Call(ctx, zapwire.MsgSetQuasarFinalized, buf.Bytes()); err != nil {
		c.logger.Warn("zap set-quasar-finalized failed", "height", height, "error", err)
	}
}

// LastQuasarHeight returns the plugin VM's accept-tip-CLAMPED Quasar EXPORT-FINAL
// height across the ZAP boundary (0 before the first export forms). Returns 0 if
// the plugin did not advertise CapQuasarExport or if the call fails — so the
// chain manager's boot re-seed of the consensus export frontier treats an
// unavailable/absent height as "empty" (advance-only; a fresh cert refines it).
func (c *Client) LastQuasarHeight() uint64 {
	if !c.quasarCapable.Load() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), quasarCallTimeout)
	defer cancel()
	respType, respData, err := c.conn.Call(ctx, zapwire.MsgQuasarHeight, nil)
	if err != nil {
		c.logger.Warn("zap quasar-height failed", "error", err)
		return 0
	}
	if respType&^(zapwire.MsgResponseFlag|zapwire.MsgErrorFlag) != zapwire.MsgQuasarHeight {
		c.logger.Warn("zap quasar-height: unexpected response type", "respType", respType)
		return 0
	}
	resp := &zapwire.QuasarHeightResponse{}
	if err := resp.Decode(zapwire.NewReader(respData)); err != nil {
		c.logger.Warn("zap quasar-height decode failed", "error", err)
		return 0
	}
	return resp.Height
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

	if _, _, err := b.client.conn.Call(ctx, zapwire.MsgBlockAccept, buf.Bytes()); err != nil {
		return err
	}
	// Refresh the cache so LastAccepted() reflects this accept instead of freezing at the
	// Initialize snapshot. Only on SUCCESS — a failed accept did not advance the plugin.
	b.client.setLastAccepted(b.id)
	return nil
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

// errorFromZAP turns the wire's verdict on a reply into an error. Absence and
// not-implemented resolve to the names the rest of the tree already uses for
// them: both are conditions a caller acts on rather than merely reports, and
// errors.Is is how it reads them.
func errorFromZAP(err zapwire.Error) error {
	switch err {
	case zapwire.ErrorUnspecified:
		return nil
	case zapwire.ErrorClosed:
		return errors.New("vm closed")
	case zapwire.ErrorNotFound:
		return database.ErrNotFound
	case zapwire.ErrorStateSyncNotImplemented:
		return chain.ErrStateSyncableVMNotImplemented
	case zapwire.ErrorInternal:
		return errors.New("vm failed")
	default:
		return fmt.Errorf("unknown error: %d", err)
	}
}

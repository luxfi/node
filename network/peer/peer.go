// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/log"

	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/bloom"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/version"
	"github.com/luxfi/utils"
)

const (
	// maxBloomSaltLen restricts the allowed size of the bloom salt to prevent
	// excessively expensive bloom filter contains checks.
	maxBloomSaltLen = 32
	// maxNumTrackedChains limits how many chains a peer can track to prevent
	// excessive memory usage.
	maxNumTrackedChains = 16

	disconnectingLog         = "disconnecting from peer"
	failedToCreateMessageLog = "failed to create message"
	failedToSetDeadlineLog   = "failed to set connection deadline"
	failedToGetUptimeLog     = "failed to get peer uptime percentage"
	malformedMessageLog      = "malformed message"
)

var (
	errClosed = errors.New("closed")

	_ Peer = (*peer)(nil)
)

// Peer encapsulates all of the functionality required to send and receive
// messages with a remote peer.
type Peer interface {
	// ID returns the nodeID of the remote peer.
	ID() ids.NodeID

	// Cert returns the certificate that the remote peer is using to
	// authenticate their messages.
	Cert() *staking.Certificate

	// LastSent returns the last time a message was sent to the peer.
	LastSent() time.Time

	// LastReceived returns the last time a message was received from the peer.
	LastReceived() time.Time

	// Ready returns true if the peer has finished the p2p handshake and is
	// ready to send and receive messages.
	Ready() bool

	// AwaitReady will block until the peer has finished the p2p handshake. If
	// the context is cancelled or the peer starts closing, then an error will
	// be returned.
	AwaitReady(ctx context.Context) error

	// Info returns a description of the state of this peer. It should only be
	// called after [Ready] returns true.
	Info() Info

	// IP returns the claimed IP and signature provided by this peer during the
	// handshake. It should only be called after [Ready] returns true.
	IP() *SignedIP

	// Version returns the claimed node version this peer is running. It should
	// only be called after [Ready] returns true.
	Version() *version.Application

	// TrackedChains returns the chains this peer is running. It should only
	// be called after [Ready] returns true.
	TrackedChains() set.Set[ids.ID]

	// ChainState reports what this peer said about ONE chain: nothing
	// (ChainUnknown), the same chain we run (ChainCompatible), or a different
	// one (ChainIncompatible). Only ChainIncompatible excludes anything, and
	// only for that chain.
	ChainState(chainID ids.ID) ChainState

	// ObservedUptime returns the local node's primary network uptime according to the
	// peer. The value ranges from [0, 100]. It should only be called after
	// [Ready] returns true.
	ObservedUptime() uint32

	// Send attempts to send [msg] to the peer. The peer takes ownership of
	// [msg] for reference counting. This returns false if the message is
	// guaranteed not to be delivered to the peer.
	Send(ctx context.Context, msg message.OutboundMessage) bool

	// StartSendGetPeerList attempts to send a GetPeerList message to this peer
	// on this peer's gossip routine. It is not guaranteed that a GetPeerList
	// will be sent.
	StartSendGetPeerList()

	// StartClose will begin shutting down the peer. It will not block.
	StartClose()

	// Closed returns true once the peer has been fully shutdown. It is
	// guaranteed that no more messages will be received by this peer once this
	// returns true.
	Closed() bool

	// AwaitClosed will block until the peer has been fully shutdown. If the
	// context is cancelled, then an error will be returned.
	AwaitClosed(ctx context.Context) error
}

type peer struct {
	*Config

	// the connection object that is used to read/write messages from
	conn net.Conn

	// [cert] is this peer's certificate, specifically the leaf of the
	// certificate chain they provided.
	cert *staking.Certificate

	// node ID of this peer.
	id ids.NodeID

	// queue of messages to send to this peer.
	messageQueue MessageQueue

	// ip is the claimed IP the peer gave us in the Handshake message.
	ip *SignedIP
	// version is the claimed version the peer is running that we received in
	// the Handshake message.
	version *version.Application
	// trackedChains are the chainIDs the peer sent us in the Handshake
	// message. The primary network ID is always included.
	trackedChains set.Set[ids.ID]
	// chainStates is what this peer said about each chain we both run. Absent
	// means it said nothing, which is ChainUnknown and is permitted.
	//
	// Written once, on the reader goroutine, while handling the Handshake, then
	// read by the sender goroutine and by peer selection; published atomically
	// and never mutated in place.
	//
	// Per CONNECTION, deliberately. A peer whose configuration is corrected
	// reconnects and is judged again from what it says then — a verdict cached
	// against a nodeID would outlive the mistake, and an operator who fixed the
	// problem would have no way to see that they had.
	chainStates utils.Atomic[map[ids.ID]ChainState]
	// mismatchSeries are the ChainIdentityMismatch label sets this connection
	// raised, kept so close() can lower them. The gauge reads "a peer is
	// currently stating a different chain", so it has to stop reading 1 when
	// that peer is gone — otherwise correcting a misconfigured node leaves its
	// alarm standing forever and the operator cannot tell that they fixed it.
	mismatchSeries utils.Atomic[[]metric.Labels]
	// options of LPs provided in the Handshake message.
	supportedLPs set.Set[uint32]
	objectedLPs  set.Set[uint32]

	// txIDOfVerifiedBLSKey is the txID that added the BLS key that was most
	// recently verified to have signed the IP.
	//
	// Invariant: Prior to the handshake being completed, this can only be
	// accessed by the reader goroutine. After the handshake has been completed,
	// this can only be accessed by the message sender goroutine.
	txIDOfVerifiedBLSKey ids.ID

	// Our primary network uptime perceived by the peer
	observedUptime utils.Atomic[uint32]

	// True if this peer has sent us a valid Handshake message and
	// is running a compatible version.
	// Only modified on the connection's reader routine.
	gotHandshake utils.Atomic[bool]

	// True if the peer:
	// * Has sent us a Handshake message
	// * Has sent us a PeerList message
	// * Is running a compatible version
	// Only modified on the connection's reader routine.
	finishedHandshake utils.Atomic[bool]

	// onFinishHandshake is closed when the peer finishes the p2p handshake.
	onFinishHandshake chan struct{}

	// numExecuting is the number of goroutines this peer is currently using
	numExecuting     int64
	startClosingOnce sync.Once
	// onClosingCtx is canceled when the peer starts closing
	onClosingCtx context.Context
	// onClosingCtxCancel cancels onClosingCtx
	onClosingCtxCancel func()

	// onClosed is closed when the peer is closed
	onClosed chan struct{}

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	lastSent, lastReceived int64

	// getPeerListChan signals that we should attempt to send a GetPeerList to
	// this peer
	getPeerListChan chan struct{}

	// isIngress is true only if the remote peer is connected to this node,
	// in contrast of this node being connected to the remote peer.
	isIngress bool

	// pqAEADKey is the 32-byte AEAD session key derived by the strict-PQ
	// application-layer handshake (ML-KEM + ML-DSA-65). Zero-valued on
	// chains that don't run the PQ handshake; non-zero only after
	// runPQHandshakeIfRequired completes successfully. The AEAD wrapper
	// over p.conn reads this key once at handshake completion; it is
	// never mutated after Start returns.
	pqAEADKey [32]byte
}

// Start a new peer instance.
//
// Invariant: There must only be one peer running at a time with a reference to
// the same [config.InboundMsgThrottler].
//
// On strict-PQ chains (config.PQHandshakeConfig non-nil) Start runs the
// application-layer ML-KEM + ML-DSA-65 handshake synchronously on the wire
// before launching the message-pump goroutines. The role is derived from
// isIngress — the dial side is the initiator, the accept side is the
// responder. If the PQ handshake fails the connection is closed and the
// returned Peer is in the closed state; no fallback to bare TLS.
func Start(
	config *Config,
	conn net.Conn,
	cert *staking.Certificate,
	id ids.NodeID,
	messageQueue MessageQueue,
	isIngress bool,
	pq *PQPreHandshake,
) Peer {
	onClosingCtx, onClosingCtxCancel := context.WithCancel(context.Background())
	p := &peer{
		isIngress:          isIngress,
		Config:             config,
		conn:               conn,
		cert:               cert,
		id:                 id,
		messageQueue:       messageQueue,
		onFinishHandshake:  make(chan struct{}),
		numExecuting:       3,
		onClosingCtx:       onClosingCtx,
		onClosingCtxCancel: onClosingCtxCancel,
		onClosed:           make(chan struct{}),
		getPeerListChan:    make(chan struct{}, 1),
		trackedChains:      make(set.Set[ids.ID]),
	}

	if isIngress {
		p.IngressConnectionCount.Add(1)
	}

	// PQ handshake gate: strict-PQ chains run the application-layer
	// ML-KEM + ML-DSA-65 handshake on the wire BEFORE any p2p message
	// is exchanged. Permissive / classical-compat chains skip this step.
	//
	// When pq != nil the caller (network.upgrade) already ran the handshake
	// over this conn BEFORE connection dedup, holding no lock — adopt its
	// result and skip the in-Start run. Running it pre-dedup is what lets
	// both ends of a simultaneous mutual dial complete independently (each
	// TCP conn has a distinct dialer=initiator / acceptor=responder); the
	// old in-Start, under-peersLock run made every node a lone initiator and
	// deadlocked. p.id was already set to the handshake-derived, key-bound
	// ML-DSA NodeID via the id param (network.upgrade sets nodeID to the
	// verified ML-DSA NodeID before calling Start), so only the AEAD key
	// needs adopting here — the binding was already enforced inside
	// RunPQHandshakeConn.
	if pq != nil {
		p.pqAEADKey = pq.AEADKey
	} else if err := p.runPQHandshakeIfRequired(); err != nil {
		p.Log.Warn("PQ handshake refused; closing connection",
			log.Stringer("nodeID", p.id),
			log.Bool("isIngress", isIngress),
			log.Reflect("error", err),
		)
		// Mark all 3 goroutines as not running so close() doesn't wait
		// for them. The connection is torn down; the peer is closed.
		atomic.StoreInt64(&p.numExecuting, 1)
		_ = p.conn.Close()
		p.close()
		return p
	}

	go p.readMessages()
	go p.writeMessages()
	go p.sendNetworkMessages()

	return p
}

// runPQHandshakeIfRequired runs the strict-PQ application-layer handshake
// synchronously over p.conn. Returns nil when the chain is on the legacy
// bare-TLS path (no PQHandshakeConfig pinned) so the caller proceeds
// unchanged. On strict-PQ, completes INIT / RESP / FINISH and stashes the
// derived AEAD key on the peer; subsequent application messages are
// authenticated against this key by the AEAD wrapper installed by the
// next CR. Wire framing mirrors the peer's existing 4-byte length-prefix
// scheme so the framing logic is identical on both sides of the wire.
func (p *peer) runPQHandshakeIfRequired() error {
	cfg := p.Config
	if cfg == nil || cfg.PQHandshakeConfig == nil || cfg.PQLocalIdentity == nil {
		return nil
	}
	peerNodeID, aeadKey, err := RunPQHandshakeConn(p.conn, cfg.PQHandshakeConfig, cfg.PQLocalIdentity, p.isIngress, cfg.MaxClockDifference)
	if err != nil {
		return err
	}
	p.pqAEADKey = aeadKey
	// Adopt the verified, key-bound ML-DSA identity authenticated by the
	// handshake as this peer's NodeID, replacing the ephemeral TLS-cert
	// NodeID from the upgrader. RunPQHandshakeConn already enforced the
	// NodeID<->ML-DSA-key binding (see adoptVerifiedPQIdentity's rationale),
	// so peerNodeID is the key-derived validator-set NodeID.
	p.Log.Debug("adopted strict-PQ peer identity from handshake",
		log.Stringer("tlsNodeID", p.id),
		log.Stringer("mldsaNodeID", peerNodeID),
		log.Bool("isIngress", p.isIngress),
	)
	p.id = peerNodeID
	return nil
}

// PQPreHandshake carries a strict-PQ handshake result computed by the caller
// (network.upgrade) BEFORE connection dedup so Start adopts it instead of
// re-running the handshake. Nil on the legacy bare-TLS path. PeerNodeID is
// the verified, key-bound ML-DSA NodeID; network.upgrade also sets the
// peer's id param to this same value, so Start need only adopt AEADKey.
type PQPreHandshake struct {
	AEADKey    [32]byte
	PeerNodeID ids.NodeID
}

// RunPQHandshakeConn runs the strict-PQ ML-KEM + ML-DSA-65 application
// handshake over conn and returns the handshake-authenticated, key-bound
// peer NodeID and derived AEAD session key. Role is by dial direction — the
// dialer (isIngress=false) initiates, the acceptor (isIngress=true) responds
// — which is well-defined per TCP connection. network.upgrade calls this
// BEFORE connection dedup so both ends of a simultaneous mutual dial
// complete independently (each conn has a distinct dialer/acceptor) and the
// dedup keys on the stable ML-DSA NodeID rather than the ephemeral TLS-cert
// NodeID. Holds no lock; bounded by a MaxClockDifference+30s deadline. Wire
// framing mirrors the peer's 4-byte length-prefix scheme (see pq_frame.go).
//
// The returned NodeID is bound to the peer's ML-DSA-65 key by
// verifyPQIdentityBinding before it is returned — the exact binding the
// in-Start adoptVerifiedPQIdentity path enforces — so a peer cannot present
// one validator's NodeID while signing with a different key.
func RunPQHandshakeConn(conn net.Conn, hs *HandshakeConfig, local *LocalIdentity, isIngress bool, maxClockDifference time.Duration) (ids.NodeID, [32]byte, error) {
	var zeroKey [32]byte
	deadline := time.Now().Add(maxClockDifference + 30*time.Second)
	if err := conn.SetDeadline(deadline); err != nil {
		return ids.EmptyNodeID, zeroKey, fmt.Errorf("set PQ handshake deadline: %w", err)
	}
	defer func() {
		// Clear deadline so the rest of the peer flow uses its own.
		_ = conn.SetDeadline(time.Time{})
	}()

	var result *HandshakeResult
	if isIngress {
		// Responder: read INIT, send RESP, derive AEAD.
		initBytes, err := readPQFrame(conn)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("read PQ INIT: %w", err)
		}
		init, err := parsePQHandshakeInit(initBytes, hs.KEMScheme)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("parse PQ INIT: %w", err)
		}
		resp, res, err := RespondHandshake(hs, local, init)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("respond PQ handshake: %w", err)
		}
		if err := writePQFrame(conn, resp.canonicalBytes()); err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("write PQ RESP: %w", err)
		}
		result = res
	} else {
		// Initiator: send INIT, read RESP, finalize.
		init, kemSec, err := InitiateHandshake(hs, local)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("initiate PQ handshake: %w", err)
		}
		if err := writePQFrame(conn, init.canonicalBytes()); err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("write PQ INIT: %w", err)
		}
		respBytes, err := readPQFrame(conn)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("read PQ RESP: %w", err)
		}
		resp, err := parsePQHandshakeResp(respBytes, hs.KEMScheme)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("parse PQ RESP: %w", err)
		}
		res, err := FinishInitiatorHandshake(hs, local, init, resp, kemSec)
		if err != nil {
			return ids.EmptyNodeID, zeroKey, fmt.Errorf("finish PQ handshake: %w", err)
		}
		result = res
	}

	// Bind the peer's claimed NodeID to the ML-DSA-65 key it proved
	// possession of, and adopt the key-derived NodeID. This is the
	// authoritative identity check (RespondHandshake / FinishInitiatorHandshake
	// only verify the signature, not the NodeID derivation).
	derived, err := verifyPQIdentityBinding(result)
	if err != nil {
		return ids.EmptyNodeID, zeroKey, err
	}
	return derived, result.AEADKey, nil
}

// verifyPQIdentityBinding binds the peer's handshake-presented NodeID to the
// ML-DSA-65 public key it proved possession of, returning the key-derived
// NodeID.
//
// The PQ handshake proves the remote holds the secret key for the ML-DSA
// public key it sent, and that it signed a transcript carrying its claimed
// NodeID. That alone is insufficient: nothing yet ties the claimed NodeID
// to that key, so a peer could sign a self-asserted NodeID (e.g. a
// validator's) with a throwaway key. We therefore re-derive the NodeID
// from the presented key under the node-identity domain — ids.Empty, the
// exact domain config.StakingConfig.DeriveNodeID uses for a node's primary
// identity (see node.Node, which sets MyNodeID = DeriveNodeID(ids.Empty)) —
// and require it to equal the claimed NodeID. The returned NodeID is the
// ML-DSA validator-set NodeID that consensus keys peers by.
func verifyPQIdentityBinding(result *HandshakeResult) (ids.NodeID, error) {
	if result == nil || result.PeerMLDSA == nil {
		return ids.EmptyNodeID, errors.New("peer: PQ handshake produced no peer identity")
	}
	derived, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, packMLDSAPub(result.PeerMLDSA))
	if err != nil {
		return ids.EmptyNodeID, fmt.Errorf("peer: derive NodeID from peer ML-DSA key: %w", err)
	}
	if derived != result.PeerNodeID {
		return ids.EmptyNodeID, fmt.Errorf(
			"peer: PQ identity binding failed — peer presented NodeID %s but its ML-DSA key derives %s",
			result.PeerNodeID, derived,
		)
	}
	return derived, nil
}

// adoptVerifiedPQIdentity binds the peer's handshake-presented NodeID to
// the ML-DSA-65 public key it proved possession of, then adopts that
// NodeID as this peer's canonical identity. See verifyPQIdentityBinding for
// the binding rationale.
//
// On success p.id is switched from the TLS-cert NodeID (a transport-layer
// artifact assigned at construction) to the ML-DSA validator-set NodeID.
// This is the fix for the strict-PQ block-production failure: consensus and
// the validator set key peers by the ML-DSA NodeID, so a peer left on its
// TLS-cert NodeID was never matched to the validator set — the P-chain saw
// zero connected validators and never produced a block. Skipped entirely on
// classical-compat chains (runPQHandshakeIfRequired returns before this).
func (p *peer) adoptVerifiedPQIdentity(result *HandshakeResult) error {
	derived, err := verifyPQIdentityBinding(result)
	if err != nil {
		return err
	}
	p.Log.Debug("adopted strict-PQ peer identity from handshake",
		log.Stringer("tlsNodeID", p.id),
		log.Stringer("mldsaNodeID", derived),
		log.Bool("isIngress", p.isIngress),
	)
	p.id = derived
	return nil
}

func (p *peer) ID() ids.NodeID {
	return p.id
}

func (p *peer) Cert() *staking.Certificate {
	return p.cert
}

func (p *peer) LastSent() time.Time {
	return time.Unix(
		atomic.LoadInt64(&p.lastSent),
		0,
	)
}

func (p *peer) LastReceived() time.Time {
	return time.Unix(
		atomic.LoadInt64(&p.lastReceived),
		0,
	)
}

func (p *peer) Ready() bool {
	return p.finishedHandshake.Get()
}

func (p *peer) AwaitReady(ctx context.Context) error {
	select {
	case <-p.onFinishHandshake:
		return nil
	case <-p.onClosed:
		return errClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *peer) Info() Info {
	primaryUptime := p.ObservedUptime()

	ip, _ := endpoints.ParseAddrPort(p.conn.RemoteAddr().String())
	return Info{
		IP:             ip,
		PublicIP:       p.ip.AddrPort,
		ID:             p.id,
		Version:        p.version.String(),
		LastSent:       p.LastSent(),
		LastReceived:   p.LastReceived(),
		ObservedUptime: json.Uint32(primaryUptime),
		TrackedChains:  p.trackedChains,
		SupportedLPs:   p.supportedLPs,
		ObjectedLPs:    p.objectedLPs,
	}
}

func (p *peer) IP() *SignedIP {
	return p.ip
}

func (p *peer) Version() *version.Application {
	return p.version
}

func (p *peer) TrackedChains() set.Set[ids.ID] {
	return p.trackedChains
}

func (p *peer) ObservedUptime() uint32 {
	return p.observedUptime.Get()
}

func (p *peer) Send(ctx context.Context, msg message.OutboundMessage) bool {
	return p.messageQueue.Push(ctx, msg)
}

func (p *peer) StartSendGetPeerList() {
	select {
	case p.getPeerListChan <- struct{}{}:
	default:
	}
}

func (p *peer) StartClose() {
	p.startClosingOnce.Do(func() {
		if p.conn != nil {
			if err := p.conn.Close(); err != nil {
				if !p.Log.IsZero() {
					p.Log.Debug("failed to close connection",
						log.Stringer("nodeID", p.id),
						log.Reflect("error", err),
					)
				}
			}
		}

		if p.messageQueue != nil {
			p.messageQueue.Close()
		}
		if p.onClosingCtxCancel != nil {
			p.onClosingCtxCancel()
		}
	})
}

func (p *peer) Closed() bool {
	select {
	case _, ok := <-p.onClosed:
		return !ok
	default:
		return false
	}
}

func (p *peer) AwaitClosed(ctx context.Context) error {
	select {
	case <-p.onClosed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// close should be called at the end of each goroutine that has been spun up.
// When the last goroutine is exiting, the peer will be marked as closed.
func (p *peer) close() {
	if atomic.AddInt64(&p.numExecuting, -1) != 0 {
		return
	}

	if p.isIngress {
		p.IngressConnectionCount.Add(-1)
	}

	// A peer that is gone is not currently stating anything.
	for _, labels := range p.mismatchSeries.Get() {
		p.Metrics.ChainIdentityMismatch.With(labels).Set(0)
	}

	p.Network.Disconnected(p.id)
	close(p.onClosed)
}

// Read and handle messages from this peer.
// When this method returns, the connection is closed.
func (p *peer) readMessages() {
	// Track this node with the inbound message throttler.
	p.InboundMsgThrottler.AddNode(p.id)
	defer func() {
		p.InboundMsgThrottler.RemoveNode(p.id)
		p.StartClose()
		p.close()
	}()

	// Continuously read and handle messages from this peer.
	reader := bufio.NewReaderSize(p.conn, p.Config.ReadBufferSize)
	msgLenBytes := make([]byte, wrappers.IntLen)
	for {
		// Time out and close connection if we can't read the message length
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.Log.Debug(failedToSetDeadlineLog,
				log.Stringer("nodeID", p.id),
				log.String("direction", "read"),
				log.Reflect("error", err),
			)
			return
		}

		// Read the message length
		if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
			p.Log.Debug("error reading message length",
				log.Stringer("nodeID", p.id),
				log.Reflect("error", err),
			)
			return
		}

		// Parse the message length
		msgLen, err := readMsgLen(msgLenBytes, constants.DefaultMaxMessageSize)
		if err != nil {
			p.Log.Debug("error parsing message length",
				log.Stringer("nodeID", p.id),
				log.Reflect("error", err),
			)
			return
		}

		// Wait until the throttler says we can proceed to read the message.
		//
		// Invariant: When done processing this message, onFinishedHandling() is
		// called exactly once. If this is not honored, the message throttler
		// will leak until no new messages can be read. You can look at message
		// throttler metrics to verify that there is no leak.
		//
		// Invariant: There must only be one call to Acquire at any given time
		// with the same nodeID. In this package, only this goroutine ever
		// performs Acquire. Additionally, we ensure that this goroutine has
		// exited before calling [Network.Disconnected] to guarantee that there
		// can't be multiple instances of this goroutine running over different
		// peer instances.
		onFinishedHandling := p.InboundMsgThrottler.Acquire(
			p.onClosingCtx,
			uint64(msgLen),
			p.id,
		)

		// If the peer is shutting down, there's no need to read the message.
		if err := p.onClosingCtx.Err(); err != nil {
			onFinishedHandling()
			return
		}

		// Time out and close connection if we can't read message
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.Log.Debug(failedToSetDeadlineLog,
				log.Stringer("nodeID", p.id),
				log.String("direction", "read"),
				log.Reflect("error", err),
			)
			onFinishedHandling()
			return
		}

		// Read the message
		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(reader, msgBytes); err != nil {
			p.Log.Debug("error reading message",
				log.Stringer("nodeID", p.id),
				log.Reflect("error", err),
			)
			onFinishedHandling()
			return
		}

		// Track the time it takes from now until the time the message is
		// handled (in the event this message is handled at the network level)
		// or the time the message is handed to the router (in the event this
		// message is not handled at the network level.)
		// Resource tracking is now handled internally by ResourceTracker

		p.Log.Debug("parsing message",
			log.Stringer("nodeID", p.id),
			log.Binary("messageBytes", msgBytes),
		)

		// Parse the message
		msg, err := p.MessageCreator.Parse(msgBytes, p.id, onFinishedHandling)
		if err != nil {
			p.Log.Debug("failed to parse message",
				log.Stringer("nodeID", p.id),
				log.Binary("messageBytes", msgBytes),
				log.Reflect("error", err),
			)

			p.Metrics.NumFailedToParse.Inc()

			// Couldn't parse the message. Read the next one.
			onFinishedHandling()
			continue
		}

		now := p.Clock.Time()
		p.storeLastReceived(now)
		p.Metrics.Received(msg, msgLen)

		// Handle the message. Note that when we are done handling this message,
		// we must call [msg.OnFinishedHandling()].
		p.handle(msg)
	}
}

func (p *peer) writeMessages() {
	defer func() {
		p.StartClose()
		p.close()
	}()

	writer := bufio.NewWriterSize(p.conn, p.Config.WriteBufferSize)

	// Make sure that the Handshake is the first message sent
	mySignedIP, err := p.IPSigner.GetSignedIP()
	if err != nil {
		p.Log.Error("failed to get signed IP",
			log.Stringer("nodeID", p.id),
			log.Reflect("error", err),
		)
		return
	}
	if port := mySignedIP.AddrPort.Port(); port == 0 {
		p.Log.Error("signed IP has invalid port",
			log.Stringer("nodeID", p.id),
			log.Uint16("port", port),
		)
		return
	}

	myVersion := p.VersionCompatibility.Version()
	knownPeersFilter, knownPeersSalt := p.Network.KnownPeers()

	var myChains []*p2p.ChainIdentity
	if p.MyChainIdentities != nil {
		for _, c := range p.MyChainIdentities.List() {
			myChains = append(myChains, c.wire())
		}
	}

	_, areWeAPrimaryNetworkValidator := p.Validators.GetValidator(constants.PrimaryNetworkID, p.MyNodeID)
	msg, err := p.MessageCreator.Handshake(
		p.NetworkID,
		p.Clock.Unix(),
		mySignedIP.AddrPort,
		myVersion.Name,
		uint32(myVersion.Major),
		uint32(myVersion.Minor),
		uint32(myVersion.Patch),
		mySignedIP.Timestamp,
		mySignedIP.TLSSignature,
		mySignedIP.BLSSignatureBytes,
		p.MyChains.List(),
		p.SupportedLPs,
		p.ObjectedLPs,
		knownPeersFilter,
		knownPeersSalt,
		areWeAPrimaryNetworkValidator,
		mySignedIP.MLDSASignature,
		myChains,
	)
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.Reflect("error", err),
		)
		return
	}
	p.writeMessage(writer, msg)

	for {
		msg, ok := p.messageQueue.PopNow()
		if ok {
			p.writeMessage(writer, msg)
			continue
		}

		// Make sure the peer was fully sent all prior messages before
		// blocking.
		if err := writer.Flush(); err != nil {
			p.Log.Debug("failed to flush writer",
				log.Stringer("nodeID", p.id),
				log.Reflect("error", err),
			)
			return
		}
		msg, ok = p.messageQueue.Pop()
		if !ok {
			// This peer is closing
			return
		}

		p.writeMessage(writer, msg)
	}
}

func (p *peer) writeMessage(writer io.Writer, msg message.OutboundMessage) {
	msgBytes := msg.Bytes()
	p.Log.Verbo("sending message",
		log.Stringer("op", msg.Op()),
		log.Stringer("nodeID", p.id),
		log.Binary("messageBytes", msgBytes),
	)

	if err := p.conn.SetWriteDeadline(p.nextTimeout()); err != nil {
		p.Log.Debug(failedToSetDeadlineLog,
			log.Stringer("nodeID", p.id),
			log.String("direction", "write"),
			log.Reflect("error", err),
		)
		return
	}

	msgLen := uint32(len(msgBytes))
	msgLenBytes, err := writeMsgLen(msgLen, constants.DefaultMaxMessageSize)
	if err != nil {
		p.Log.Debug("error writing message length",
			log.Stringer("nodeID", p.id),
			log.Reflect("error", err),
		)
		return
	}

	// Write the message
	var buf net.Buffers = [][]byte{msgLenBytes[:], msgBytes}
	if _, err := io.CopyN(writer, &buf, int64(wrappers.IntLen+msgLen)); err != nil {
		p.Log.Debug("error writing message",
			log.Stringer("nodeID", p.id),
			log.Reflect("error", err),
		)
		return
	}
	now := p.Clock.Time()
	p.storeLastSent(now)
	p.Metrics.Sent(msg)
}

func (p *peer) sendNetworkMessages() {
	sendPingsTicker := time.NewTicker(p.PingFrequency)
	defer func() {
		sendPingsTicker.Stop()

		p.StartClose()
		p.close()
	}()

	for {
		select {
		case <-p.getPeerListChan:
			knownPeersFilter, knownPeersSalt := p.Config.Network.KnownPeers()
			_, areWeAPrimaryNetworkValidator := p.Validators.GetValidator(constants.PrimaryNetworkID, p.MyNodeID)
			msg, err := p.Config.MessageCreator.GetPeerList(
				knownPeersFilter,
				knownPeersSalt,
				areWeAPrimaryNetworkValidator,
			)
			if err != nil {
				p.Log.Error(failedToCreateMessageLog,
					log.Stringer("nodeID", p.id),
					log.Stringer("messageOp", message.GetPeerListOp),
					log.Reflect("error", err),
				)
				return
			}

			p.Send(p.onClosingCtx, msg)
		case <-sendPingsTicker.C:
			if !p.Network.AllowConnection(p.id) {
				p.Log.Debug(disconnectingLog,
					log.String("reason", "connection is no longer desired"),
					log.Stringer("nodeID", p.id),
				)
				return
			}

			// Only check if we should disconnect after the handshake is
			// finished to avoid race conditions and accessing uninitialized
			// values.
			if p.finishedHandshake.Get() && p.shouldDisconnect() {
				return
			}

			primaryUptime := p.getUptime()
			pingMessage, err := p.MessageCreator.Ping(primaryUptime)
			if err != nil {
				p.Log.Error(failedToCreateMessageLog,
					log.Stringer("nodeID", p.id),
					log.Stringer("messageOp", message.PingOp),
					log.Reflect("error", err),
				)
				return
			}

			p.Send(p.onClosingCtx, pingMessage)
		case <-p.onClosingCtx.Done():
			return
		}
	}
}

// shouldDisconnect is called both during receipt of the Handshake message and
// periodically when sending a Ping message (after finishing the handshake!).
//
// It is called during the Handshake to prevent marking a peer as connected and
// then immediately disconnecting from them.
//
// It is called when sending a Ping message to account for validator set
// changes. It's called when sending a Ping rather than in a validator set
// callback to avoid signature verification on the P-chain accept path.
func (p *peer) shouldDisconnect() bool {
	if err := p.VersionCompatibility.Compatible(p.version); err != nil {
		p.Log.Debug(disconnectingLog,
			log.String("reason", "version not compatible"),
			log.Stringer("nodeID", p.id),
			log.Stringer("peerVersion", p.version),
			log.Reflect("error", err),
		)
		return true
	}

	// Enforce that all validators that have registered a BLS key are signing
	// their IP with the signed-IP payload.
	vdr, ok := p.Validators.GetValidator(constants.PrimaryNetworkID, p.id)
	if !ok || vdr.PublicKey == nil || vdr.TxID == p.txIDOfVerifiedBLSKey {
		return false
	}

	// Convert []byte public key to *bls.PublicKey
	// Validator's public key is stored in uncompressed format (96 bytes)
	blsPublicKey := bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey)
	if blsPublicKey == nil {
		p.Log.Debug(disconnectingLog,
			log.String("reason", "invalid BLS public key"),
			log.Stringer("nodeID", p.id),
		)
		return true
	}

	validSignature := bls.VerifyProofOfPossession(
		blsPublicKey,
		p.ip.BLSSignature,
		p.ip.UnsignedIP.bytes(),
	)
	if !validSignature {
		p.Log.Debug(disconnectingLog,
			log.String("reason", "invalid BLS signature"),
			log.Stringer("nodeID", p.id),
		)
		return true
	}

	// Avoid unnecessary signature verifications by only verifying the signature
	// once per validation period.
	p.txIDOfVerifiedBLSKey = vdr.TxID
	return false
}

func (p *peer) handle(msg message.InboundMessage) {
	switch m := msg.Message().(type) { // Network-related message types
	case *p2p.Ping:
		p.handlePing(m)
		msg.OnFinishedHandling()
		return
	case *p2p.Pong:
		p.handlePong(m)
		msg.OnFinishedHandling()
		return
	case *p2p.Handshake:
		p.handleHandshake(m)
		msg.OnFinishedHandling()
		return
	case *p2p.GetPeerList:
		p.handleGetPeerList(m)
		msg.OnFinishedHandling()
		return
	case *p2p.PeerList:
		p.handlePeerList(m)
		msg.OnFinishedHandling()
		return
	}
	if !p.finishedHandshake.Get() {
		p.Log.Debug("dropping message",
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", msg.Op()),
			log.String("reason", "handshake isn't finished"),
		)
		msg.OnFinishedHandling()
		return
	}

	// Consensus and app-level messages. A peer running a different chain is not
	// heard on it — this is the inbound half of the exclusion; the outbound half
	// is peer selection in network.getPeers/samplePeers, so neither node spends
	// a poll on the other.
	if chainID, err := message.GetChainID(msg.Message()); err == nil && p.ChainState(chainID) == ChainIncompatible {
		p.Log.Debug("dropping message",
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", msg.Op()),
			log.Stringer("chainID", chainID),
			log.String("reason", "peer is running a different chain"),
		)
		p.Metrics.ChainDivergentMsgs.Inc()
		msg.OnFinishedHandling()
		return
	}

	// Route the message to the application layer handler
	p.Router.HandleInbound(context.Background(), msg)
	msg.OnFinishedHandling()
}

// compareChains decides, per blockchain, whether this peer is running the same
// chain we are, and records the ones it is not.
//
// ABSENCE IS NOT DISAGREEMENT. A peer that states nothing for a chain — because
// it does not run that chain, or because it predates this field entirely — is
// UNKNOWN, and unknown peers participate exactly as they did before. Only a
// statement that is PRESENT AND DIFFERENT excludes anything.
//
// That asymmetry is deliberate and it is the thing not to "tighten" later. The
// handshake wire format is positional, so a peer too old to carry the field and a
// peer that chose to say nothing are literally the same bytes; there is no way to
// tell them apart, and treating silence as disagreement therefore means treating
// every not-yet-upgraded node as a stranger. On a five-node fleet upgraded one
// node at a time — which is the only safe way to upgrade one, since the fleet
// cannot lose quorum — the first node to get the new build would exclude the four
// still carrying consensus and isolate itself. The check would become the outage
// it exists to prevent. Once every node states its chains, nothing is silent and
// the tolerance costs nothing.
//
// A disagreement is loud, because it is never routine: it means two nodes are
// building on different histories and one of them is misconfigured.
func (p *peer) compareChains(stated []*p2p.ChainIdentity) {
	if p.MyChainIdentities == nil {
		return
	}

	states := make(map[ids.ID]ChainState, len(stated))
	var raised []metric.Labels
	for _, w := range stated {
		theirs, err := parseChainIdentity(w)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.HandshakeOp),
				log.String("field", "chains"),
				log.Reflect("error", err),
			)
			p.StartClose()
			return
		}

		// A chain we do not run is not ours to have an opinion about.
		ours, run := p.MyChainIdentities.Get(theirs.ChainID)
		if !run {
			continue
		}

		if ours.Agrees(theirs) {
			states[theirs.ChainID] = ChainCompatible
			if ours.Rules != theirs.Rules {
				// Reported, never acted on: a rule generation is a claim about
				// the peer's binary that this node cannot check. Worth seeing
				// early — two nodes on one chain with different scheduled rules
				// are compatible now and will fork when those rules apply — but
				// acting on it would let a peer decide whether we participate.
				p.Metrics.ChainRulesDiffer.Inc()
				p.Log.Info("peer runs the same chain under a different rule generation",
					log.Stringer("nodeID", p.id),
					log.Stringer("chainID", theirs.ChainID),
					log.String("localRules", fmt.Sprintf("%x", ours.Rules)),
					log.String("peerRules", fmt.Sprintf("%x", theirs.Rules)),
				)
			}
			continue
		}

		states[theirs.ChainID] = ChainIncompatible
		labels := metric.Labels{
			"peer":          p.id.String(),
			"chain":         theirs.ChainID.String(),
			"local_genesis": fmt.Sprintf("%x", ours.Genesis),
			"peer_genesis":  fmt.Sprintf("%x", theirs.Genesis),
		}
		p.Metrics.ChainIdentityMismatch.With(labels).Set(1)
		raised = append(raised, labels)
		p.Log.Warn("peer is running a different chain; excluding it from this chain only",
			log.Stringer("nodeID", p.id),
			log.Stringer("chainID", theirs.ChainID),
			log.String("difference", ours.Disagreement(theirs)),
			log.String("localGenesis", fmt.Sprintf("%x", ours.Genesis)),
			log.String("peerGenesis", fmt.Sprintf("%x", theirs.Genesis)),
		)
	}
	p.chainStates.Set(states)
	p.mismatchSeries.Set(raised)
}

// ChainState reports what this peer said about one chain. The answer drives both
// directions identically: an incompatible peer is neither heard on that chain nor
// spoken to on it, so neither node wastes a poll on the other and neither can be
// reached by the other's blocks. One-directional exclusion would be worse than
// none — writing a peer off while still accepting its blocks, or the reverse.
func (p *peer) ChainState(chainID ids.ID) ChainState {
	return p.chainStates.Get()[chainID]
}

func (p *peer) handlePing(msg *p2p.Ping) {
	if msg.Uptime > 100 {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PingOp),
			log.Stringer("netID", constants.PrimaryNetworkID),
			log.Uint32("uptime", msg.Uptime),
		)
		p.StartClose()
		return
	}
	p.observedUptime.Set(msg.Uptime)

	pongMessage, err := p.MessageCreator.Pong()
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PongOp),
			log.Reflect("error", err),
		)
		p.StartClose()
		return
	}

	p.Send(p.onClosingCtx, pongMessage)
}

func (p *peer) getUptime() uint32 {
	primaryUptime, err := p.UptimeCalculator.CalculateUptimePercent(
		p.id,
		constants.PrimaryNetworkID,
	)
	if err != nil {
		p.Log.Debug(failedToGetUptimeLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("netID", constants.PrimaryNetworkID),
			log.Reflect("error", err),
		)
		primaryUptime = 0
	}

	primaryUptimePercent := uint32(primaryUptime * 100)
	return primaryUptimePercent
}

func (*peer) handlePong(*p2p.Pong) {}

func (p *peer) handleHandshake(msg *p2p.Handshake) {
	if p.gotHandshake.Get() {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("reason", "already received handshake"),
		)
		p.StartClose()
		return
	}

	if msg.NetworkId != p.NetworkID {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "networkID"),
			log.Uint32("peerNetworkID", msg.NetworkId),
			log.Uint32("ourNetworkID", p.NetworkID),
		)
		p.StartClose()
		return
	}

	localTime := p.Clock.Time()
	localUnixTime := uint64(localTime.Unix())
	clockDifference := math.Abs(float64(msg.MyTime) - float64(localUnixTime))

	p.Metrics.ClockSkewCount.Inc()
	p.Metrics.ClockSkewSum.Add(clockDifference)

	if clockDifference > p.MaxClockDifference.Seconds() {
		logFunc := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			logFunc = p.Log.Warn
		}
		logFunc(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "myTime"),
			log.Uint64("peerTime", msg.MyTime),
			log.Uint64("localTime", localUnixTime),
		)
		p.StartClose()
		return
	}

	p.version = &version.Application{
		Name:  msg.Client.GetName(),
		Major: int(msg.Client.GetMajor()),
		Minor: int(msg.Client.GetMinor()),
		Patch: int(msg.Client.GetPatch()),
	}

	if p.VersionCompatibility.Version().Before(p.version) {
		logFunc := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			logFunc = p.Log.Info
		}
		logFunc("peer attempting to connect with newer version. You may want to update your client",
			log.Stringer("nodeID", p.id),
			log.Stringer("peerVersion", p.version),
		)
	}

	// handle chain IDs
	if numTrackedChains := len(msg.TrackedNets); numTrackedChains > maxNumTrackedChains {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "trackedChains"),
			log.Int("numTrackedChains", numTrackedChains),
		)
		p.StartClose()
		return
	}

	p.trackedChains.Add(constants.PrimaryNetworkID)
	for _, chainIDBytes := range msg.TrackedNets {
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.HandshakeOp),
				log.String("field", "trackedChains"),
				log.Reflect("error", err),
			)
			p.StartClose()
			return
		}
		p.trackedChains.Add(chainID)
	}

	p.compareChains(msg.GetChains())

	for _, lp := range msg.SupportedLps {
		if constants.CurrentLPs.Contains(lp) {
			p.supportedLPs.Add(lp)
		}
	}
	for _, lp := range msg.ObjectedLps {
		if constants.CurrentLPs.Contains(lp) {
			p.objectedLPs.Add(lp)
		}
	}

	if p.supportedLPs.Overlaps(p.objectedLPs) {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "lps"),
			log.Reflect("supportedLPs", p.supportedLPs),
			log.Reflect("objectedLPs", p.objectedLPs),
		)
		p.StartClose()
		return
	}

	var (
		knownPeers = bloom.EmptyFilter
		salt       []byte
	)
	if msg.KnownPeers != nil {
		var err error
		knownPeers, err = bloom.Parse(msg.KnownPeers.Filter)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.HandshakeOp),
				log.String("field", "knownPeers.filter"),
				log.Reflect("error", err),
			)
			p.StartClose()
			return
		}

		salt = msg.KnownPeers.Salt
		if saltLen := len(salt); saltLen > maxBloomSaltLen {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.HandshakeOp),
				log.String("field", "knownPeers.salt"),
				log.Int("saltLen", saltLen),
			)
			p.StartClose()
			return
		}
	}

	addr, ok := endpoints.AddrFromSlice(msg.IpAddr)
	if !ok {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "ip"),
			log.Int("ipLen", len(msg.IpAddr)),
		)
		p.StartClose()
		return
	}

	port, validPort := wirePort(msg.IpPort)
	if !validPort {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "port"),
			log.Uint16("port", port),
		)
		p.StartClose()
		return
	}

	p.ip = &SignedIP{
		UnsignedIP: UnsignedIP{
			AddrPort: netip.AddrPortFrom(
				addr,
				port,
			),
			Timestamp: msg.IpSigningTime,
		},
		TLSSignature: msg.IpNodeIdSig,
		// Empty on legacy peers (classical-only); the SignedIP.Verify
		// call below only enforces the ML-DSA leg under strict-PQ
		// profile and only when the validator's ML-DSA public key is
		// known to the verifier. The wire format tolerates absence.
		MLDSASignature: msg.GetIpMldsaSig(),
	}
	maxTimestamp := localTime.Add(p.MaxClockDifference)
	if err := p.ip.Verify(p.cert, maxTimestamp); err != nil {
		logFunc := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			logFunc = p.Log.Warn
		}
		logFunc(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "tlsSignature"),
			log.Uint64("peerTime", msg.MyTime),
			log.Uint64("localTime", localUnixTime),
			log.Reflect("error", err),
		)

		p.StartClose()
		return
	}

	signature, err := bls.SignatureFromBytes(msg.IpBlsSig)
	if err != nil {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.HandshakeOp),
			log.String("field", "blsSignature"),
			log.Reflect("error", err),
		)
		p.StartClose()
		return
	}

	p.ip.BLSSignature = signature
	p.ip.BLSSignatureBytes = msg.IpBlsSig

	// If the peer is running an incompatible version or has an invalid BLS
	// signature, disconnect from them prior to marking the handshake as
	// completed.
	if p.shouldDisconnect() {
		p.StartClose()
		return
	}

	p.gotHandshake.Set(true)

	peerIPs := p.Network.Peers(p.id, p.trackedChains, msg.AllChains, knownPeers, salt)

	// We bypass throttling here to ensure that the handshake message is
	// acknowledged correctly.
	peerListMsg, err := p.Config.MessageCreator.PeerList(peerIPs, true /*=bypassThrottling*/)
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PeerListOp),
			log.Reflect("error", err),
		)
		p.StartClose()
		return
	}

	if !p.Send(p.onClosingCtx, peerListMsg) {
		// Because throttling was marked to be bypassed with this message,
		// sending should only fail if the peer has started closing.
		p.Log.Debug("failed to send reliable message",
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PeerListOp),
			log.Reflect("error", p.onClosingCtx.Err()),
		)
		p.StartClose()
	}
}

func (p *peer) handleGetPeerList(msg *p2p.GetPeerList) {
	if !p.finishedHandshake.Get() {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.GetPeerListOp),
			log.String("reason", "not finished handshake"),
		)
		return
	}

	knownPeersMsg := msg.GetKnownPeers()
	filter, err := bloom.Parse(knownPeersMsg.GetFilter())
	if err != nil {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.GetPeerListOp),
			log.String("field", "knownPeers.filter"),
			log.Reflect("error", err),
		)
		p.StartClose()
		return
	}

	salt := knownPeersMsg.GetSalt()
	if saltLen := len(salt); saltLen > maxBloomSaltLen {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.GetPeerListOp),
			log.String("field", "knownPeers.salt"),
			log.Int("saltLen", saltLen),
		)
		p.StartClose()
		return
	}

	peerIPs := p.Network.Peers(p.id, p.trackedChains, msg.AllChains, filter, salt)
	if len(peerIPs) == 0 {
		p.Log.Debug("skipping sending of empty peer list",
			log.Stringer("nodeID", p.id),
		)
		return
	}

	// Bypass throttling is disabled here to follow the non-handshake message
	// sending pattern.
	peerListMsg, err := p.Config.MessageCreator.PeerList(peerIPs, false /*=bypassThrottling*/)
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PeerListOp),
			log.Reflect("error", err),
		)
		return
	}

	p.Send(p.onClosingCtx, peerListMsg)
}

func (p *peer) handlePeerList(msg *p2p.PeerList) {
	if !p.finishedHandshake.Get() {
		if !p.gotHandshake.Get() {
			return
		}

		p.Network.Connected(p.id)
		p.finishedHandshake.Set(true)
		close(p.onFinishHandshake)
	}

	discoveredIPs := make([]*endpoints.ClaimedIPPort, len(msg.ClaimedIpPorts)) // the peers this peer told us about
	for i, claimedIPPort := range msg.ClaimedIpPorts {
		tlsCert, err := staking.ParseCertificate(claimedIPPort.X509Certificate)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.PeerListOp),
				log.String("field", "cert"),
				log.Reflect("error", err),
			)
			p.StartClose()
			return
		}

		addr, ok := endpoints.AddrFromSlice(claimedIPPort.IpAddr)
		if !ok {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.PeerListOp),
				log.String("field", "ip"),
				log.Int("ipLen", len(claimedIPPort.IpAddr)),
			)
			p.StartClose()
			return
		}

		port, validPort := wirePort(claimedIPPort.IpPort)
		if !validPort {
			p.Log.Debug(malformedMessageLog,
				log.Stringer("nodeID", p.id),
				log.Stringer("messageOp", message.PeerListOp),
				log.String("field", "port"),
				log.Uint16("port", port),
			)
			p.StartClose()
			return
		}

		discoveredIPs[i] = endpoints.NewClaimedIPPort(
			tlsCert,
			netip.AddrPortFrom(
				addr,
				port,
			),
			claimedIPPort.Timestamp,
			claimedIPPort.Signature,
		)
	}

	if err := p.Network.Track(discoveredIPs); err != nil {
		p.Log.Debug(malformedMessageLog,
			log.Stringer("nodeID", p.id),
			log.Stringer("messageOp", message.PeerListOp),
			log.String("field", "claimedIP"),
			log.Reflect("error", err),
		)
	}
}

func (p *peer) nextTimeout() time.Time {
	return p.Clock.Time().Add(p.PongTimeout)
}

func (p *peer) storeLastSent(time time.Time) {
	unixTime := time.Unix()
	atomic.StoreInt64(&p.Config.LastSent, unixTime)
	atomic.StoreInt64(&p.lastSent, unixTime)
}

func (p *peer) storeLastReceived(time time.Time) {
	unixTime := time.Unix()
	atomic.StoreInt64(&p.Config.LastReceived, unixTime)
	atomic.StoreInt64(&p.lastReceived, unixTime)
}

// wirePort narrows a port carried as a wire uint32 to the uint16 an address
// actually holds, reporting whether it is usable. A value that does not fit in
// uint16 is rejected rather than truncated: 65536 would otherwise narrow to 0
// and be accepted as a port, then be re-gossiped as 0 and get us closed by every
// peer that receives it. Port 0 is never a valid peer port.
func wirePort(p uint32) (uint16, bool) {
	if p == 0 || p > math.MaxUint16 {
		return 0, false
	}
	return uint16(p), true
}

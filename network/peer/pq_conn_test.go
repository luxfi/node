// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/kem"
)

// boundIdentity returns an ML-DSA-65 identity whose NodeID is the canonical
// derivation of its own public key — what a real staking key gives you, and
// the only shape RunPQHandshakeConn accepts.
func boundIdentity(t *testing.T) *LocalIdentity {
	t.Helper()
	id, err := NewLocalIdentity(ids.EmptyNodeID)
	require.NoError(t, err)
	id.NodeID = deriveStakingNodeID(t, id)
	return id
}

// pqConnResult is what one side of a conn handshake reports back.
type pqConnResult struct {
	nodeID ids.NodeID
	key    [32]byte
	err    error
}

// runConnHandshake drives both roles concurrently over a socket pair and
// returns (dialer, acceptor) results. A real pipe, because the framing and the
// deadline are as much part of the handshake as the crypto is.
func runConnHandshake(t *testing.T, dialCfg, acceptCfg *HandshakeConfig, dialID, acceptID *LocalIdentity) (pqConnResult, pqConnResult) {
	t.Helper()
	dialConn, acceptConn := net.Pipe()
	defer dialConn.Close()
	defer acceptConn.Close()

	out := make(chan pqConnResult, 2)
	go func() {
		// A peer that refuses the handshake drops the connection; without
		// that, the other side sits on the handshake deadline instead.
		defer acceptConn.Close()
		nodeID, key, err := RunPQHandshakeConn(acceptConn, acceptCfg, acceptID, true /*isIngress*/, time.Second)
		out <- pqConnResult{nodeID, key, err}
	}()
	nodeID, key, err := RunPQHandshakeConn(dialConn, dialCfg, dialID, false /*isIngress*/, time.Second)
	dialer := pqConnResult{nodeID, key, err}

	select {
	case acceptor := <-out:
		return dialer, acceptor
	case <-time.After(30 * time.Second):
		t.Fatal("acceptor never returned")
		return dialer, pqConnResult{}
	}
}

// TestPQConn_BothSidesAgreeOnKeyAndIdentity is the whole handshake in one
// property: after a run over a real socket the two peers hold the same AEAD
// key and each names the other by the NodeID that its ML-DSA key derives.
// Same key or the connection is deaf; right NodeID or consensus is talking to
// someone it cannot identify.
func TestPQConn_BothSidesAgreeOnKeyAndIdentity(t *testing.T) {
	for _, scheme := range []kem.KeyExchangeID{kem.KeyExchangeMLKEM768, kem.KeyExchangeMLKEM1024} {
		require := require.New(t)

		var chainID [chainIDSize]byte
		copy(chainID[:], "a chain both of them are running")
		cfg := pqTestConfig(chainID, scheme)

		dialID, acceptID := boundIdentity(t), boundIdentity(t)
		dialer, acceptor := runConnHandshake(t, cfg, cfg, dialID, acceptID)

		require.NoError(dialer.err)
		require.NoError(acceptor.err)
		require.Equal(dialer.key, acceptor.key, "scheme=%s: both sides must derive one key", scheme)
		require.NotEqual([32]byte{}, dialer.key, "scheme=%s: the derived key must not be zero", scheme)
		require.Equal(acceptID.NodeID, dialer.nodeID, "the dialer must learn the acceptor's key-derived NodeID")
		require.Equal(dialID.NodeID, acceptor.nodeID, "the acceptor must learn the dialer's key-derived NodeID")
	}
}

// TestPQConn_EveryRunDerivesAFreshKey pins forward secrecy at the session
// level: the ML-DSA identity is long-term, the ML-KEM keypair is not. Two
// handshakes between the same two nodes must not produce the same key, or
// recording one session decrypts every later one.
func TestPQConn_EveryRunDerivesAFreshKey(t *testing.T) {
	require := require.New(t)

	var chainID [chainIDSize]byte
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)
	dialID, acceptID := boundIdentity(t), boundIdentity(t)

	first, _ := runConnHandshake(t, cfg, cfg, dialID, acceptID)
	second, _ := runConnHandshake(t, cfg, cfg, dialID, acceptID)
	require.NoError(first.err)
	require.NoError(second.err)
	require.NotEqual(first.key, second.key,
		"two sessions between the same identities must not share a key")
}

// TestPQConn_UnboundNodeIDIsRefusedOnTheWire is the impersonation case end to
// end. The peer holds a perfectly good key and signs everything correctly —
// it just claims a NodeID that key does not derive, which is how you would
// present yourself as a validator you are not. The signature check passes and
// the binding check is what must refuse.
func TestPQConn_UnboundNodeIDIsRefusedOnTheWire(t *testing.T) {
	require := require.New(t)

	var chainID [chainIDSize]byte
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	honest := boundIdentity(t)
	liar := boundIdentity(t)
	liar.NodeID = ids.GenerateTestNodeID() // a NodeID its key does not derive

	// The liar dials; the honest acceptor must refuse it.
	_, acceptor := runConnHandshake(t, cfg, cfg, liar, honest)
	require.Error(acceptor.err)
	require.Contains(acceptor.err.Error(), "identity binding failed")
	require.Equal(ids.EmptyNodeID, acceptor.nodeID)
	require.Equal([32]byte{}, acceptor.key, "a refused handshake must yield no session key")

	// And the same the other way: the liar accepting must be refused by the
	// honest dialer.
	dialer, _ := runConnHandshake(t, cfg, cfg, honest, liar)
	require.Error(dialer.err)
	require.Contains(dialer.err.Error(), "identity binding failed")
}

// TestPQConn_ChainMismatchRefused keeps two different chains from ever sharing
// a session. The chain id is in the transcript and in the key derivation, so
// nodes on different chains must not merely fail to agree — they must be told
// no, by name, at the handshake.
func TestPQConn_ChainMismatchRefused(t *testing.T) {
	require := require.New(t)

	var ours, theirs [chainIDSize]byte
	copy(ours[:], "chain A")
	copy(theirs[:], "chain B")

	dialer, acceptor := runConnHandshake(t,
		pqTestConfig(ours, kem.KeyExchangeMLKEM768),
		pqTestConfig(theirs, kem.KeyExchangeMLKEM768),
		boundIdentity(t), boundIdentity(t))

	require.ErrorIs(acceptor.err, ErrHandshakeChainMismatch)
	require.Error(dialer.err, "the dialer must not walk away believing it has a session")
}

// TestPQConn_KEMMismatchRefused covers the negotiation-free rule: there is no
// downgrade path, so a peer offering a scheme we do not run is refused rather
// than accommodated.
func TestPQConn_KEMMismatchRefused(t *testing.T) {
	require := require.New(t)

	var chainID [chainIDSize]byte
	dialer, acceptor := runConnHandshake(t,
		pqTestConfig(chainID, kem.KeyExchangeMLKEM768),
		pqTestConfig(chainID, kem.KeyExchangeMLKEM1024),
		boundIdentity(t), boundIdentity(t))

	require.ErrorIs(acceptor.err, ErrHandshakeKEMScheme)
	require.Error(dialer.err)
}

// TestPQConn_GarbageFromThePeerIsRefusedNotParsed points a hostile sender at
// the acceptor and requires that no run of bytes it chooses ends in a session.
// The acceptor speaks first only after it has read a whole INIT, so anything
// short, oversize or simply wrong has to come back as an error.
func TestPQConn_GarbageFromThePeerIsRefusedNotParsed(t *testing.T) {
	var chainID [chainIDSize]byte
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	scripts := map[string][]byte{
		"empty":                     {},
		"partial header":            {0x00, 0x00},
		"header only":               header(64),
		"header then short body":    append(header(64), 1, 2, 3),
		"length above the cap":      header(pqFrameMaxSize + 1),
		"length is max uint32":      header(^uint32(0)),
		"empty frame":               header(0),
		"frame of random bytes":     append(header(8), 1, 2, 3, 4, 5, 6, 7, 8),
		"frame of zeros at the cap": append(header(1024), make([]byte, 1024)...),
	}

	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			serverConn, clientConn := net.Pipe()
			defer serverConn.Close()

			go func() {
				defer clientConn.Close()
				_, _ = clientConn.Write(script)
				// Hold the conn open briefly so a reader waiting for more
				// bytes sees a deadline rather than an instant EOF.
				time.Sleep(50 * time.Millisecond)
			}()

			nodeID, key, err := RunPQHandshakeConn(serverConn, cfg, boundIdentity(t), true /*isIngress*/, 100*time.Millisecond)
			require.Error(err, "hostile bytes must never produce a session")
			require.Equal(ids.EmptyNodeID, nodeID)
			require.Equal([32]byte{}, key)
		})
	}
}

// TestPQConn_DeadlineIsClearedForTheRestOfTheConnection guards a bug that only
// shows up hours later: the handshake sets a deadline on the shared conn, and
// if it leaves it set every subsequent read on that peer dies at a fixed wall
// clock time. Whether the handshake succeeded or failed, the deadline belongs
// to the handshake alone.
func TestPQConn_DeadlineIsClearedForTheRestOfTheConnection(t *testing.T) {
	require := require.New(t)

	var chainID [chainIDSize]byte
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM768)

	tracked := &deadlineConn{}
	// No peer on the other end, so this fails at the first read — the path
	// that most easily forgets to clean up.
	_, _, err := RunPQHandshakeConn(tracked, cfg, boundIdentity(t), true, time.Second)
	require.Error(err)
	require.True(tracked.lastDeadline.IsZero(),
		"the handshake must clear its deadline before handing the conn back")
	require.GreaterOrEqual(tracked.sets, 2, "expected a deadline set and then cleared")
}

// deadlineConn records deadline changes and reads nothing.
type deadlineConn struct {
	scriptConn
	lastDeadline time.Time
	sets         int
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.lastDeadline = t
	c.sets++
	return nil
}

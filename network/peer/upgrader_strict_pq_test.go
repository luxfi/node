// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"net"
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/node/network/peer"
)

// TestUpgrader_StrictPQ_RefusesClassicalTLS — closes BLOCKERS CR-3.
// Constructs a tlsServerUpgrader wired with a strict-PQ peer.SchemeGate
// (ActivationHeight=0 — gate active from genesis) and dials it with a
// vanilla RSA TLS client. The server-side upgrade MUST refuse the
// connection at the SchemeGate, not later in some peer-init step. The
// returned error MUST wrap peer.ErrSchemeGateMismatch with site="handshake"
// so a log reader can identify the refusing boundary.
func TestUpgrader_StrictPQ_RefusesClassicalTLS(t *testing.T) {
	require := require.New(t)

	// Strict-PQ SchemeGate active from genesis. Forward-only PQ —
	// the gate refuses the classical scheme byte at every height.
	gate, err := peer.NewSchemeGate(consensusconfig.StrictPQ(), 0)
	require.NoError(err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(err)
	serverCert := makeTLSCert(t, serverKey)
	serverTLS := peer.TLSConfig(serverCert, nil)

	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(err)
	clientCert := makeTLSCert(t, clientKey)

	invalidCertCounter := metric.NewCounter(metric.CounterOpts{})
	upgrader := peer.NewTLSServerUpgrader(serverTLS, invalidCertCounter, gate)

	clientConfig := tls.Config{
		ClientAuth:         tls.RequireAnyClientCert,
		InsecureSkipVerify: true, //#nosec G402 — staking TLS, trust derives from cert hash
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{clientCert},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer listener.Close()

	eg := &errgroup.Group{}
	eg.Go(func() error {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		_, _, _, upgradeErr := upgrader.Upgrade(conn)
		return upgradeErr
	})

	conn, err := tls.Dial("tcp", listener.Addr().String(), &clientConfig)
	require.NoError(err)
	require.NoError(conn.Handshake())

	err = eg.Wait()
	require.Error(err, "strict-PQ SchemeGate MUST refuse classical RSA cert")
	require.ErrorIs(err, peer.ErrSchemeGateMismatch,
		"refusal MUST come from the SchemeGate, not later peer-init")
	require.Contains(err.Error(), "site=handshake",
		"refusal MUST name the handshake boundary in the error")
}

// TestUpgrader_NilGate_AcceptsClassicalTLS — the negative control. Without
// a SchemeGate (legacy / classical-compat networks), the upgrader accepts
// the same RSA TLS connection that the strict-PQ test refuses. This keeps
// the gate's behaviour orthogonal to the existing TLS path.
func TestUpgrader_NilGate_AcceptsClassicalTLS(t *testing.T) {
	require := require.New(t)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(err)
	serverCert := makeTLSCert(t, serverKey)
	serverTLS := peer.TLSConfig(serverCert, nil)

	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(err)
	clientCert := makeTLSCert(t, clientKey)

	upgrader := peer.NewTLSServerUpgrader(
		serverTLS,
		metric.NewCounter(metric.CounterOpts{}),
		nil, // no SchemeGate
	)

	clientConfig := tls.Config{
		ClientAuth:         tls.RequireAnyClientCert,
		InsecureSkipVerify: true, //#nosec G402
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{clientCert},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer listener.Close()

	eg := &errgroup.Group{}
	eg.Go(func() error {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		_, _, _, upgradeErr := upgrader.Upgrade(conn)
		return upgradeErr
	})

	conn, err := tls.Dial("tcp", listener.Addr().String(), &clientConfig)
	require.NoError(err)
	require.NoError(conn.Handshake())

	require.NoError(eg.Wait(), "nil gate MUST accept classical TLS as today")
}

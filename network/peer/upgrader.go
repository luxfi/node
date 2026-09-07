// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"net"

	"github.com/luxfi/metric"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
)

var (
	errNoCert = errors.New("tls handshake finished with no peer certificate")

	_ Upgrader = (*tlsServerUpgrader)(nil)
	_ Upgrader = (*tlsClientUpgrader)(nil)
)

type Upgrader interface {
	// Must be thread safe
	Upgrade(net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error)
}

type tlsServerUpgrader struct {
	config       *tls.Config
	invalidCerts metric.Counter
	gate         *SchemeGate
}

// NewTLSServerUpgrader returns an Upgrader that runs the server-side TLS
// handshake and (when gate is non-nil) the cross-axis NodeIDScheme gate
// against the chain's locked ChainSecurityProfile. A nil gate disables the
// cross-axis check; this is the legacy / classical-compat behaviour.
func NewTLSServerUpgrader(config *tls.Config, invalidCerts metric.Counter, gate *SchemeGate) Upgrader {
	return &tlsServerUpgrader{
		config:       config,
		invalidCerts: invalidCerts,
		gate:         gate,
	}
}

func (t *tlsServerUpgrader) Upgrade(conn net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	return connToIDAndCert(tls.Server(conn, t.config), t.invalidCerts, t.gate)
}

type tlsClientUpgrader struct {
	config       *tls.Config
	invalidCerts metric.Counter
	gate         *SchemeGate
}

// NewTLSClientUpgrader returns an Upgrader that runs the client-side TLS
// handshake and (when gate is non-nil) the cross-axis NodeIDScheme gate.
// A nil gate disables the cross-axis check.
func NewTLSClientUpgrader(config *tls.Config, invalidCerts metric.Counter, gate *SchemeGate) Upgrader {
	return &tlsClientUpgrader{
		config:       config,
		invalidCerts: invalidCerts,
		gate:         gate,
	}
}

func (t *tlsClientUpgrader) Upgrade(conn net.Conn) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	return connToIDAndCert(tls.Client(conn, t.config), t.invalidCerts, t.gate)
}

// connToIDAndCert runs the TLS handshake, parses the peer leaf certificate,
// derives the canonical NodeID, and (when gate is non-nil) refuses the
// connection at the cross-axis gate before any downstream wiring sees it.
// Refusal here is by design: the gate is the single place that says "this
// peer's NodeIDScheme is not admissible on this chain"; later boundaries
// trust that an admitted peer has already passed.
func connToIDAndCert(conn *tls.Conn, invalidCerts metric.Counter, gate *SchemeGate) (ids.NodeID, net.Conn, *staking.Certificate, error) {
	if err := conn.Handshake(); err != nil {
		return ids.EmptyNodeID, nil, nil, err
	}

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return ids.EmptyNodeID, nil, nil, errNoCert
	}

	tlsCert := state.PeerCertificates[0]
	peerCert, err := staking.ParseCertificate(tlsCert.Raw)
	if err != nil {
		invalidCerts.Inc()
		return ids.EmptyNodeID, nil, nil, err
	}

	nodeID := ids.NodeIDFromCert(&ids.Certificate{
		Raw:       peerCert.Raw,
		PublicKey: peerCert.PublicKey,
	})

	if gate != nil {
		scheme := schemeFromCert(peerCert)
		if _, err := gate.Classify(nodeID, scheme, 0, "handshake"); err != nil {
			return ids.EmptyNodeID, nil, nil, err
		}
	}

	return nodeID, conn, peerCert, nil
}

// schemeFromCert derives the wire NodeIDScheme byte from a parsed TLS leaf
// certificate. TLS staking certs today carry RSA or ECDSA public keys —
// both classify as NodeIDSchemeSecp256k1 (the canonical classical-compat
// byte). A future ML-DSA cert extension (an X.509 extension carrying the
// FIPS 204 public key) is recognised by its OID; until that extension
// lands the function returns the classical byte. The gate's profile pin
// is what decides admission — this function only names the byte.
func schemeFromCert(cert *staking.Certificate) ids.NodeIDScheme {
	if cert == nil || cert.PublicKey == nil {
		return ids.NodeIDSchemeInvalid
	}
	switch cert.PublicKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		return ids.NodeIDSchemeSecp256k1
	default:
		return ids.NodeIDSchemeInvalid
	}
}

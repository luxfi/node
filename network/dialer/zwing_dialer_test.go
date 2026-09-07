// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/zwing"
)

func newPair(t *testing.T) (client, server *zwing.Identity) {
	t.Helper()
	c, err := zwing.GenerateIdentity()
	if err != nil {
		t.Fatalf("client identity: %v", err)
	}
	s, err := zwing.GenerateIdentity()
	if err != nil {
		t.Fatalf("server identity: %v", err)
	}
	return c, s
}

func TestZWingDialerMissingIdentity(t *testing.T) {
	base := NewEndpointDialer("tcp", EndpointDialerConfig{}, log.NewNoOpLogger())
	if _, err := NewZWingDialer(base, ZWingDialerConfig{}, log.NewNoOpLogger()); !errors.Is(err, ErrZWingMissingIdentity) {
		t.Fatalf("expected ErrZWingMissingIdentity, got %v", err)
	}
}

func TestZWingListenerMissingIdentity(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if _, err := NewZWingListener(ln, ZWingDialerConfig{}, log.NewNoOpLogger()); !errors.Is(err, ErrZWingMissingIdentity) {
		t.Fatalf("expected ErrZWingMissingIdentity, got %v", err)
	}
}

// TestZWingDialerOverTCP wires a real TCP listener + Z-Wing listener and
// verifies the dialer produces a post-handshake net.Conn that round-trips
// application bytes.
func TestZWingDialerOverTCP(t *testing.T) {
	clientID, serverID := newPair(t)

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer rawLn.Close()

	zwLn, err := NewZWingListener(rawLn, ZWingDialerConfig{LocalIdentity: serverID}, log.NewNoOpLogger())
	if err != nil {
		t.Fatalf("zwing listener: %v", err)
	}

	type accepted struct {
		c   net.Conn
		err error
	}
	acceptCh := make(chan accepted, 1)
	go func() {
		c, err := zwLn.Accept()
		acceptCh <- accepted{c: c, err: err}
	}()

	base := NewEndpointDialer("tcp", EndpointDialerConfig{}, log.NewNoOpLogger())
	zd, err := NewZWingDialer(base, ZWingDialerConfig{LocalIdentity: clientID}, log.NewNoOpLogger())
	if err != nil {
		t.Fatalf("zwing dialer: %v", err)
	}

	addr := netip.MustParseAddrPort(rawLn.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cConn, err := zd.Dial(ctx, addr, serverID.Public())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cConn.Close()

	a := <-acceptCh
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	defer a.c.Close()

	payload := []byte("zwing-over-tcp via dialer")
	wErr := make(chan error, 1)
	go func() { _, e := cConn.Write(payload); wErr <- e }()
	buf := make([]byte, len(payload))
	if _, err := a.c.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-wErr; err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("got %q want %q", buf, payload)
	}
}

// TestZWingDialerWrapAnyConn proves Z-Wing rides on top of any net.Conn,
// so RNS mesh links (or Unix sockets, or pipes) get the same secure
// channel as TCP without changes to this layer.
func TestZWingDialerWrapAnyConn(t *testing.T) {
	clientID, serverID := newPair(t)

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	cZD, err := NewZWingDialer(
		NewEndpointDialer("tcp", EndpointDialerConfig{}, log.NewNoOpLogger()),
		ZWingDialerConfig{LocalIdentity: clientID},
		log.NewNoOpLogger(),
	)
	if err != nil {
		t.Fatalf("client zwing dialer: %v", err)
	}

	type result struct {
		conn net.Conn
		err  error
	}
	cCh := make(chan result, 1)
	sCh := make(chan result, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c, err := cZD.Wrap(left, serverID.Public())
		cCh <- result{conn: c, err: err}
	}()
	go func() {
		defer wg.Done()
		serverWrapper, _ := NewZWingListener(stubListener{}, ZWingDialerConfig{LocalIdentity: serverID}, log.NewNoOpLogger())
		c, err := zwing.Server(right, serverWrapper.cfg)
		sCh <- result{conn: c, err: err}
	}()
	wg.Wait()

	c := <-cCh
	s := <-sCh
	if c.err != nil || s.err != nil {
		t.Fatalf("handshake: client=%v server=%v", c.err, s.err)
	}
	defer c.conn.Close()
	defer s.conn.Close()

	payload := []byte("zwing-over-pipe")
	go func() { _, _ = c.conn.Write(payload) }()
	buf := make([]byte, len(payload))
	if _, err := s.conn.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("got %q want %q", buf, payload)
	}
}

// stubListener satisfies net.Listener for the Wrap-side construction.
type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, errors.New("stub") }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &stubAddr{} }

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub" }

// TestZWingDialerIdentityMismatch confirms a wrong pinned remote rejects
// the handshake (defence against MitM).
func TestZWingDialerIdentityMismatch(t *testing.T) {
	clientID, serverID := newPair(t)
	_, otherID := newPair(t)

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer rawLn.Close()

	zwLn, _ := NewZWingListener(rawLn, ZWingDialerConfig{LocalIdentity: serverID}, log.NewNoOpLogger())
	go func() { _, _ = zwLn.Accept() }()

	base := NewEndpointDialer("tcp", EndpointDialerConfig{}, log.NewNoOpLogger())
	zd, _ := NewZWingDialer(base, ZWingDialerConfig{LocalIdentity: clientID}, log.NewNoOpLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := netip.MustParseAddrPort(rawLn.Addr().String())
	if _, err := zd.Dial(ctx, addr, otherID.Public()); err == nil {
		t.Fatal("expected ErrIdentityMismatch via wrapped error")
	}
}

// TestZWingDialerDialEndpoint exercises the endpoint-aware path so
// Z-Wing works over hostname endpoints too (and, transitively, over
// any future RNS endpoint that produces a `net.Conn`).
func TestZWingDialerDialEndpoint(t *testing.T) {
	clientID, serverID := newPair(t)

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer rawLn.Close()

	zwLn, _ := NewZWingListener(rawLn, ZWingDialerConfig{LocalIdentity: serverID}, log.NewNoOpLogger())
	go func() { _, _ = zwLn.Accept() }()

	base := NewEndpointDialer("tcp", EndpointDialerConfig{}, log.NewNoOpLogger())
	zd, _ := NewZWingDialer(base, ZWingDialerConfig{LocalIdentity: clientID}, log.NewNoOpLogger())

	endpoint := endpoints.NewIPEndpoint(netip.MustParseAddrPort(rawLn.Addr().String()))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := zd.DialEndpoint(ctx, endpoint, serverID.Public())
	if err != nil {
		t.Fatalf("dial endpoint: %v", err)
	}
	conn.Close()
}

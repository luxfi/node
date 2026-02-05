// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/endpoints"
	"github.com/stretchr/testify/require"
)

func TestRNSTransportDisabled(t *testing.T) {
	require := require.New(t)

	config := DefaultRNSConfig()
	config.Enabled = false

	transport := NewRNSTransport(config, log.NewNoOpLogger())
	require.False(transport.Available())

	dest := [endpoints.RNSDestinationLen]byte{0x01, 0x02, 0x03}
	_, err := transport.Dial(context.Background(), dest)
	require.ErrorIs(err, ErrRNSNotConfigured)
}

func TestRNSTransportEnabled(t *testing.T) {
	require := require.New(t)

	// Use temp directory for identity
	tmpDir := t.TempDir()

	config := DefaultRNSConfig()
	config.Enabled = true
	config.IdentityPath = filepath.Join(tmpDir, "identity")

	transport := NewRNSTransport(config, log.NewNoOpLogger()).(*rnsTransport)
	require.False(transport.Available()) // Not started yet

	err := transport.Start()
	require.NoError(err)
	require.True(transport.Available())

	// Verify identity was created
	require.NotNil(transport.identity)
	dest := transport.Destination()
	require.NotEqual([endpoints.RNSDestinationLen]byte{}, dest)

	// Verify identity was persisted
	_, err = os.Stat(config.IdentityPath)
	require.NoError(err)

	// Close transport
	err = transport.Close()
	require.NoError(err)
	require.False(transport.Available())
}

func TestRNSTransportDialUnknownDestination(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()
	config := DefaultRNSConfig()
	config.Enabled = true
	config.IdentityPath = filepath.Join(tmpDir, "identity")

	transport := NewRNSTransport(config, log.NewNoOpLogger()).(*rnsTransport)
	err := transport.Start()
	require.NoError(err)
	defer transport.Close()

	// Dialing an unknown destination without gateway should fail
	dest := [endpoints.RNSDestinationLen]byte{
		0xa5, 0xf7, 0x2c, 0x3d, 0x4e, 0x5f, 0x60, 0x71,
		0x82, 0x93, 0xa4, 0xb5, 0xc6, 0xd7, 0xe8, 0xf9,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = transport.Dial(ctx, dest)
	require.Error(err)
	require.ErrorIs(err, ErrDestinationUnknown)
}

func TestRNSIdentityPersistence(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()
	identityPath := filepath.Join(tmpDir, "identity")

	// Create first identity
	id1, err := LoadOrGenerateIdentity(identityPath)
	require.NoError(err)
	require.NotNil(id1)
	dest1 := id1.Destination()

	// Load same identity
	id2, err := LoadOrGenerateIdentity(identityPath)
	require.NoError(err)
	require.NotNil(id2)
	dest2 := id2.Destination()

	// Should be identical
	require.Equal(dest1, dest2)
}

func TestRNSLinkHandshake(t *testing.T) {
	require := require.New(t)

	// Create two identities
	id1, err := NewRNSIdentity()
	require.NoError(err)

	id2, err := NewRNSIdentity()
	require.NoError(err)

	// Create a pipe for testing
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Set reasonable deadlines
	clientConn.SetDeadline(time.Now().Add(5 * time.Second))
	serverConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Create links
	clientLink := NewRNSLink(clientConn, id1)
	serverLink := NewRNSLink(serverConn, id2)

	// Run handshakes concurrently
	errCh := make(chan error, 2)

	go func() {
		errCh <- clientLink.Handshake(true, id2.Destination())
	}()

	go func() {
		errCh <- serverLink.Handshake(false, id1.Destination())
	}()

	// Wait for both handshakes
	for i := 0; i < 2; i++ {
		err := <-errCh
		require.NoError(err, "handshake %d failed", i)
	}

	// Verify links are established
	require.True(clientLink.IsEstablished())
	require.True(serverLink.IsEstablished())

	// Test encrypted communication
	testMsg := []byte("hello from client")
	go func() {
		_, err := clientLink.Write(testMsg)
		require.NoError(err)
	}()

	recvBuf := make([]byte, 100)
	n, err := serverLink.Read(recvBuf)
	require.NoError(err)
	require.Equal(testMsg, recvBuf[:n])

	// Test reverse direction
	replyMsg := []byte("hello from server")
	go func() {
		_, err := serverLink.Write(replyMsg)
		require.NoError(err)
	}()

	n, err = clientLink.Read(recvBuf)
	require.NoError(err)
	require.Equal(replyMsg, recvBuf[:n])
}

func TestRNSAnnouncer(t *testing.T) {
	require := require.New(t)

	// Create identity
	id, err := NewRNSIdentity()
	require.NoError(err)

	// Create announcer
	config := DefaultRNSAnnouncerConfig()
	config.AnnounceInterval = 100 * time.Millisecond
	announcer := NewRNSAnnouncer(id, config)

	err = announcer.Start()
	require.NoError(err)
	defer announcer.Stop()

	// Register handler
	receivedCh := make(chan [endpoints.RNSDestinationLen]byte, 1)
	announcer.RegisterHandler(func(dest [endpoints.RNSDestinationLen]byte, entry *AnnounceEntry) {
		select {
		case receivedCh <- dest:
		default:
		}
	})

	// Add an entry manually
	id2, err := NewRNSIdentity()
	require.NoError(err)

	entry := &AnnounceEntry{
		Destination: id2.Destination(),
		SigningKey:  id2.SigningPublicKey(),
		ExchangeKey: id2.X25519PublicKey(),
		LastSeen:    time.Now(),
		ExpiresAt:   time.Now().Add(30 * time.Minute),
	}
	announcer.AddEntry(entry)

	// Lookup should succeed
	found, err := announcer.LookupEntry(id2.Destination())
	require.NoError(err)
	require.NotNil(found)
	require.Equal(entry.Destination, found.Destination)

	// Lookup unknown should fail
	unknownDest := [endpoints.RNSDestinationLen]byte{0xff, 0xff, 0xff, 0xff}
	_, err = announcer.LookupEntry(unknownDest)
	require.ErrorIs(err, ErrDestinationUnknown)
}

func TestRNSConnClosed(t *testing.T) {
	require := require.New(t)

	// Create a minimal rnsConn for testing close behavior
	conn := &rnsConn{
		destination: [endpoints.RNSDestinationLen]byte{0x01},
		link:        nil, // No link, tests guard conditions
	}

	// Should be able to close without link
	err := conn.Close()
	require.NoError(err)

	// Operations on closed conn should fail
	_, err = conn.Write([]byte("test"))
	require.ErrorIs(err, ErrRNSLinkClosed)

	_, err = conn.Read(make([]byte, 10))
	require.ErrorIs(err, ErrRNSLinkClosed)
}

func TestRNSAddr(t *testing.T) {
	require := require.New(t)

	// Local address
	local := &rnsAddr{local: true}
	require.Equal("rns", local.Network())
	require.Equal("rns://local", local.String())

	// Remote address
	dest := [endpoints.RNSDestinationLen]byte{
		0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22, 0x33,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
	}
	remote := &rnsAddr{destination: dest}
	require.Equal("rns", remote.Network())
	require.Equal("rns://deadbeef00112233445566778899aabb", remote.String())
}

func TestDestinationHex(t *testing.T) {
	tests := []struct {
		dest [endpoints.RNSDestinationLen]byte
		want string
	}{
		{
			[endpoints.RNSDestinationLen]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			"00000000000000000000000000000000",
		},
		{
			[endpoints.RNSDestinationLen]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			"ffffffffffffffffffffffffffffffff",
		},
		{
			[endpoints.RNSDestinationLen]byte{0xa5, 0xf7, 0x2c, 0x3d, 0x4e, 0x5f, 0x60, 0x71, 0x82, 0x93, 0xa4, 0xb5, 0xc6, 0xd7, 0xe8, 0xf9},
			"a5f72c3d4e5f60718293a4b5c6d7e8f9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := destinationHex(tt.dest)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEndpointDialerWithRNS(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	// Create RNS transport
	rnsConfig := DefaultRNSConfig()
	rnsConfig.Enabled = true
	rnsConfig.IdentityPath = filepath.Join(tmpDir, "identity")

	transport := NewRNSTransport(rnsConfig, log.NewNoOpLogger()).(*rnsTransport)
	err := transport.Start()
	require.NoError(err)
	defer transport.Close()

	// Create endpoint dialer with RNS
	config := EndpointDialerConfig{
		Config: Config{
			ThrottleRps:       0,
			ConnectionTimeout: 5 * time.Second,
		},
		RNSTransport: transport,
	}

	dialer := NewEndpointDialer("tcp", config, log.NewNoOpLogger()).(*endpointDialer)
	require.True(dialer.RNSAvailable())
}

func TestEndpointDialerWithoutRNS(t *testing.T) {
	require := require.New(t)

	// Create endpoint dialer without RNS
	config := EndpointDialerConfig{
		Config: Config{
			ThrottleRps:       0,
			ConnectionTimeout: 5 * time.Second,
		},
	}

	dialer := NewEndpointDialer("tcp", config, log.NewNoOpLogger()).(*endpointDialer)
	require.False(dialer.RNSAvailable())

	// RNS dial should fail
	dest := [endpoints.RNSDestinationLen]byte{0x01, 0x02, 0x03}
	endpoint := endpoints.NewRNSEndpoint(dest)

	_, err := dialer.DialEndpoint(context.Background(), endpoint)
	require.ErrorIs(err, ErrRNSNotConfigured)
}

func TestEndpointDialerSetRNSTransport(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	// Create dialer without RNS
	config := EndpointDialerConfig{
		Config: Config{
			ThrottleRps:       0,
			ConnectionTimeout: 5 * time.Second,
		},
	}

	dialer := NewEndpointDialer("tcp", config, log.NewNoOpLogger()).(*endpointDialer)
	require.False(dialer.RNSAvailable())

	// Add RNS transport dynamically
	rnsConfig := DefaultRNSConfig()
	rnsConfig.Enabled = true
	rnsConfig.IdentityPath = filepath.Join(tmpDir, "identity")

	transport := NewRNSTransport(rnsConfig, log.NewNoOpLogger()).(*rnsTransport)
	err := transport.Start()
	require.NoError(err)
	defer transport.Close()

	dialer.SetRNSTransport(transport)
	require.True(dialer.RNSAvailable())
}

func TestRNSIdentitySignVerify(t *testing.T) {
	require := require.New(t)

	id, err := NewRNSIdentity()
	require.NoError(err)

	// Sign a message
	msg := []byte("test message to sign")
	sig := id.Sign(msg)
	require.NotEmpty(sig)

	// Verify with own public key
	require.True(id.Verify(msg, sig))

	// Verify with VerifyWithPublicKey
	require.True(VerifyWithPublicKey(id.PublicKey(), msg, sig))

	// Modify message, should fail
	require.False(id.Verify([]byte("tampered message"), sig))

	// Modify signature, should fail
	badSig := make([]byte, len(sig))
	copy(badSig, sig)
	badSig[0] ^= 0xff
	require.False(id.Verify(msg, badSig))
}

func TestRNSIdentityEncryptDecrypt(t *testing.T) {
	require := require.New(t)

	id1, err := NewRNSIdentity()
	require.NoError(err)

	id2, err := NewRNSIdentity()
	require.NoError(err)

	// id1 encrypts to id2
	ephemeralPub, sharedSecret, err := id1.Encrypt(id2.EncryptionPublicKey())
	require.NoError(err)
	require.NotEmpty(ephemeralPub)
	require.NotEmpty(sharedSecret)

	// id2 decrypts
	decryptedSecret, err := id2.Decrypt(ephemeralPub)
	require.NoError(err)

	// Shared secrets should match
	require.Equal(sharedSecret, decryptedSecret)
}

func TestRNSIdentityX25519Exchange(t *testing.T) {
	require := require.New(t)

	id1, err := NewRNSIdentity()
	require.NoError(err)

	id2, err := NewRNSIdentity()
	require.NoError(err)

	// Both sides compute shared secret
	secret1, err := id1.X25519Exchange(id2.X25519PublicKey())
	require.NoError(err)

	secret2, err := id2.X25519Exchange(id1.X25519PublicKey())
	require.NoError(err)

	// Shared secrets should match
	require.Equal(secret1, secret2)
}

func TestParseAddrPort(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"127.0.0.1:8080", true},
		{"[::1]:8080", true},
		{"localhost:9651", true}, // May fail if hostname doesn't resolve
		{"", false},
		{"invalid", false},
		{"127.0.0.1:0", false},
		{"127.0.0.1:99999", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ap := parseAddrPort(tt.input)
			if tt.valid {
				// For valid inputs that don't require DNS resolution
				if tt.input == "127.0.0.1:8080" || tt.input == "[::1]:8080" {
					require.True(t, ap.IsValid(), "expected valid for %s", tt.input)
				}
			} else {
				require.False(t, ap.IsValid(), "expected invalid for %s", tt.input)
			}
		})
	}
}

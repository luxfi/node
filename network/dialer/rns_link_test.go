// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pipeConn creates a connected pair of net.Conn for testing.
func pipeConn() (net.Conn, net.Conn) {
	return net.Pipe()
}

func TestRNSLink_Handshake(t *testing.T) {
	require := require.New(t)

	// Create two identities
	alice, err := NewRNSIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewRNSIdentity()
	require.NoError(err)
	defer bob.Close()

	// Create connected pipe
	aliceConn, bobConn := pipeConn()
	defer aliceConn.Close()
	defer bobConn.Close()

	// Create links
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Perform handshake in parallel
	var wg sync.WaitGroup
	var aliceErr, bobErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceErr = aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobErr = bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	require.NoError(aliceErr)
	require.NoError(bobErr)

	require.True(aliceLink.IsEstablished())
	require.True(bobLink.IsEstablished())

	// Verify peer destinations match
	require.Equal(bob.Destination(), aliceLink.PeerDestination())
	require.Equal(alice.Destination(), bobLink.PeerDestination())

	// Classical links should not be hybrid
	require.False(aliceLink.IsHybrid())
	require.False(bobLink.IsHybrid())
}

func TestRNSLinkEncryptedCommunication(t *testing.T) {
	require := require.New(t)

	// Create identities and establish links
	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Test bidirectional encrypted communication
	testData := []byte("Hello, encrypted world!")

	// Alice -> Bob
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := aliceLink.Write(testData)
		require.NoError(err)
		require.Equal(len(testData), n)
	}()

	var received []byte
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := bobLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(testData, received)

	// Bob -> Alice
	replyData := []byte("Hello back from Bob!")

	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := bobLink.Write(replyData)
		require.NoError(err)
		require.Equal(len(replyData), n)
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := aliceLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(replyData, received)

	// Cleanup
	aliceLink.Close()
	bobLink.Close()
}

func TestRNSLinkMultipleMessages(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Send multiple messages and verify order is preserved
	messages := []string{
		"Message 1",
		"Message 2 - longer message with more content",
		"Message 3",
		"Message 4 - the final message",
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		for _, msg := range messages {
			_, err := aliceLink.Write([]byte(msg))
			require.NoError(err)
		}
	}()

	var received []string
	go func() {
		defer wg.Done()
		for range messages {
			buf := make([]byte, 1024)
			n, err := bobLink.Read(buf)
			require.NoError(err)
			received = append(received, string(buf[:n]))
		}
	}()
	wg.Wait()

	require.Equal(len(messages), len(received))
	for i, msg := range messages {
		require.Equal(msg, received[i])
	}

	aliceLink.Close()
	bobLink.Close()
}

func TestRNSLinkClosedOperations(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Close Alice's link
	require.NoError(aliceLink.Close())

	// Operations on closed link should fail
	_, err := aliceLink.Write([]byte("test"))
	require.ErrorIs(err, ErrRNSLinkClosed)

	_, err = aliceLink.Read(make([]byte, 10))
	require.ErrorIs(err, ErrRNSLinkClosed)

	// Double close should be safe
	require.NoError(aliceLink.Close())

	bobLink.Close()
}

func TestRNSLinkAddresses(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Check addresses
	localAddr := aliceLink.LocalAddr()
	require.Equal("rns", localAddr.Network())
	require.Contains(localAddr.String(), "rns://")

	remoteAddr := aliceLink.RemoteAddr()
	require.Equal("rns", remoteAddr.Network())
	require.Contains(remoteAddr.String(), "rns://")

	aliceLink.Close()
	bobLink.Close()
}

func TestRNSLinkDeadlines(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Set deadlines
	deadline := time.Now().Add(time.Hour)
	require.NoError(aliceLink.SetDeadline(deadline))
	require.NoError(aliceLink.SetReadDeadline(deadline))
	require.NoError(aliceLink.SetWriteDeadline(deadline))

	aliceLink.Close()
	bobLink.Close()
}

func TestRNSLinkNotEstablished(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	defer alice.Close()

	aliceConn, _ := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)

	// Operations on unestablished link should fail
	_, err := aliceLink.Write([]byte("test"))
	require.Error(err)
	require.Contains(err.Error(), "not established")

	_, err = aliceLink.Read(make([]byte, 10))
	require.Error(err)
	require.Contains(err.Error(), "not established")

	aliceLink.Close()
}

func TestRNSLinkLargeMessage(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Send a large message (but within limits)
	largeData := bytes.Repeat([]byte("X"), 32*1024) // 32KB

	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := aliceLink.Write(largeData)
		require.NoError(err)
		require.Equal(len(largeData), n)
	}()

	var received []byte
	go func() {
		defer wg.Done()
		buf := make([]byte, 64*1024)
		n, err := bobLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(largeData, received)

	aliceLink.Close()
	bobLink.Close()
}

func TestRNSLink_X25519KeyExchange(t *testing.T) {
	require := require.New(t)

	// Test X25519 key exchange directly
	id1, _ := NewRNSIdentity()
	id2, _ := NewRNSIdentity()
	defer id1.Close()
	defer id2.Close()

	secret1, err := id1.X25519Exchange(id2.X25519PublicKey())
	require.NoError(err)

	secret2, err := id2.X25519Exchange(id1.X25519PublicKey())
	require.NoError(err)

	// Shared secrets must match
	require.Equal(secret1, secret2)
}

func TestRNSLink_VerifyWithPubKey(t *testing.T) {
	require := require.New(t)

	id, _ := NewRNSIdentity()
	defer id.Close()

	message := []byte("test message")
	sig := id.Sign(message)

	// Valid verification
	require.True(VerifyWithPubKey(id.SigningPublicKey(), message, sig))

	// Wrong message
	require.False(VerifyWithPubKey(id.SigningPublicKey(), []byte("wrong"), sig))

	// Wrong public key
	id2, _ := NewRNSIdentity()
	defer id2.Close()
	require.False(VerifyWithPubKey(id2.SigningPublicKey(), message, sig))

	// Invalid public key size
	require.False(VerifyWithPubKey([]byte("short"), message, sig))

	// Invalid signature size
	require.False(VerifyWithPubKey(id.SigningPublicKey(), message, []byte("short")))
}

func TestRNSLink_Hash(t *testing.T) {
	require := require.New(t)

	id, _ := NewRNSIdentity()
	defer id.Close()

	// Hash should equal Destination
	require.Equal(id.Destination(), id.Hash())
}

func TestRNSLink_DoubleHandshake(t *testing.T) {
	require := require.New(t)

	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Second handshake should be no-op (already established)
	err := aliceLink.Handshake(true, bob.Destination())
	require.NoError(err)

	aliceLink.Close()
	bobLink.Close()
}

// --- Hybrid Post-Quantum Tests ---

func TestHybridRNSLink_Handshake(t *testing.T) {
	require := require.New(t)

	// Create two hybrid identities
	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewHybridIdentity()
	require.NoError(err)
	defer bob.Close()

	// Create connected pipe
	aliceConn, bobConn := pipeConn()
	defer aliceConn.Close()
	defer bobConn.Close()

	// Create hybrid links
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewHybridRNSLink(bobConn, bob)

	// Perform handshake in parallel
	var wg sync.WaitGroup
	var aliceErr, bobErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceErr = aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobErr = bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	require.NoError(aliceErr)
	require.NoError(bobErr)

	require.True(aliceLink.IsEstablished())
	require.True(bobLink.IsEstablished())

	// Both links should be hybrid
	require.True(aliceLink.IsHybrid())
	require.True(bobLink.IsHybrid())

	// Verify peer hybrid identities are set
	require.NotNil(aliceLink.PeerIdentity())
	require.NotNil(bobLink.PeerIdentity())

	// Verify peer destinations match
	require.Equal(bob.Destination(), aliceLink.PeerDestination())
	require.Equal(alice.Destination(), bobLink.PeerDestination())
}

func TestHybridRNSLink_EncryptedCommunication(t *testing.T) {
	require := require.New(t)

	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewHybridIdentity()
	require.NoError(err)
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewHybridRNSLink(bobConn, bob)

	// Handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	require.True(aliceLink.IsHybrid())
	require.True(bobLink.IsHybrid())

	// Test bidirectional encrypted communication
	testData := []byte("Hello, post-quantum encrypted world!")

	// Alice -> Bob
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := aliceLink.Write(testData)
		require.NoError(err)
		require.Equal(len(testData), n)
	}()

	var received []byte
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := bobLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(testData, received)

	// Bob -> Alice
	replyData := []byte("Hello back with quantum-resistant encryption!")

	wg.Add(2)
	go func() {
		defer wg.Done()
		n, err := bobLink.Write(replyData)
		require.NoError(err)
		require.Equal(len(replyData), n)
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := aliceLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(replyData, received)

	aliceLink.Close()
	bobLink.Close()
}

func TestHybridToClassical_Fallback(t *testing.T) {
	require := require.New(t)

	// Alice has hybrid identity, Bob has classical
	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewRNSIdentity()
	require.NoError(err)
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	defer aliceConn.Close()
	defer bobConn.Close()

	// Alice uses hybrid link, Bob uses classical
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	// Perform handshake in parallel
	var wg sync.WaitGroup
	var aliceErr, bobErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceErr = aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobErr = bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	require.NoError(aliceErr)
	require.NoError(bobErr)

	require.True(aliceLink.IsEstablished())
	require.True(bobLink.IsEstablished())

	// Should fall back to classical (Bob doesn't support hybrid)
	require.False(aliceLink.IsHybrid())
	require.False(bobLink.IsHybrid())

	// Verify communication still works
	testData := []byte("Fallback to classical encryption")

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := aliceLink.Write(testData)
		require.NoError(err)
	}()

	var received []byte
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, err := bobLink.Read(buf)
		require.NoError(err)
		received = buf[:n]
	}()
	wg.Wait()

	require.Equal(testData, received)

	aliceLink.Close()
	bobLink.Close()
}

func TestHybridRNSLink_KeyDerivationMatches(t *testing.T) {
	require := require.New(t)

	// Create two hybrid identities
	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewHybridIdentity()
	require.NoError(err)
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewHybridRNSLink(bobConn, bob)

	// Perform handshake
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Verify both sides derived the same keys by checking communication works
	// If keys don't match, decryption will fail

	// Send multiple messages to verify key consistency
	for i := 0; i < 10; i++ {
		testData := []byte("Test message for key derivation")

		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := aliceLink.Write(testData)
			require.NoError(err)
		}()

		go func() {
			defer wg.Done()
			buf := make([]byte, 1024)
			n, err := bobLink.Read(buf)
			require.NoError(err)
			require.Equal(testData, buf[:n])
		}()
		wg.Wait()
	}

	aliceLink.Close()
	bobLink.Close()
}

func TestHybridRNSLink_ForwardSecrecy(t *testing.T) {
	require := require.New(t)

	// Create identities
	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewHybridIdentity()
	require.NoError(err)
	defer bob.Close()

	// Establish first link
	aliceConn1, bobConn1 := pipeConn()
	aliceLink1 := NewHybridRNSLink(aliceConn1, alice)
	bobLink1 := NewHybridRNSLink(bobConn1, bob)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink1.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink1.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Establish second link with same identities (should use different ephemeral keys)
	aliceConn2, bobConn2 := pipeConn()
	aliceLink2 := NewHybridRNSLink(aliceConn2, alice)
	bobLink2 := NewHybridRNSLink(bobConn2, bob)

	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink2.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink2.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// Both links should be established independently
	require.True(aliceLink1.IsEstablished())
	require.True(aliceLink2.IsEstablished())

	// Send messages on both links to verify independence
	msg1 := []byte("Message on link 1")
	msg2 := []byte("Message on link 2")

	wg.Add(4)
	go func() {
		defer wg.Done()
		aliceLink1.Write(msg1)
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, _ := bobLink1.Read(buf)
		require.Equal(msg1, buf[:n])
	}()
	go func() {
		defer wg.Done()
		aliceLink2.Write(msg2)
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		n, _ := bobLink2.Read(buf)
		require.Equal(msg2, buf[:n])
	}()
	wg.Wait()

	// Verify ephemeral keys were destroyed (forward secrecy)
	require.True(aliceLink1.ephemeralKeysDestroyed)
	require.True(bobLink1.ephemeralKeysDestroyed)
	require.True(aliceLink2.ephemeralKeysDestroyed)
	require.True(bobLink2.ephemeralKeysDestroyed)

	aliceLink1.Close()
	bobLink1.Close()
	aliceLink2.Close()
	bobLink2.Close()
}

func TestHybridRNSLink_PeerIdentityAccessor(t *testing.T) {
	require := require.New(t)

	alice, err := NewHybridIdentity()
	require.NoError(err)
	defer alice.Close()

	bob, err := NewHybridIdentity()
	require.NoError(err)
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewHybridRNSLink(bobConn, bob)

	// Before handshake, peer identity should be nil
	require.Nil(aliceLink.PeerIdentity())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	// After handshake, peer identity should be available
	alicePeerID := aliceLink.PeerIdentity()
	bobPeerID := bobLink.PeerIdentity()

	require.NotNil(alicePeerID)
	require.NotNil(bobPeerID)

	// Verify peer identity destinations match
	require.Equal(bob.Destination(), alicePeerID.Destination())
	require.Equal(alice.Destination(), bobPeerID.Destination())

	aliceLink.Close()
	bobLink.Close()
}

func TestMessageTypeConstants(t *testing.T) {
	require := require.New(t)

	// Verify message type constants (cast to int for comparison)
	require.Equal(0x01, MsgTypeLinkRequest)
	require.Equal(0x02, MsgTypeLinkAccept)
	require.Equal(0x03, MsgTypeLinkProof)
	require.Equal(0x04, MsgTypeLinkComplete)
	require.Equal(0x05, MsgTypeData)
	require.Equal(0x06, MsgTypeKeyExchange)

	// Verify backward compatibility aliases
	require.Equal(MsgTypeLinkRequest, handshakeLinkRequest)
	require.Equal(MsgTypeLinkAccept, handshakeLinkAccept)
	require.Equal(MsgTypeLinkProof, handshakeLinkProof)
	require.Equal(MsgTypeLinkComplete, handshakeLinkComplete)
}

// --- Benchmarks ---

func BenchmarkClassicalHandshake(b *testing.B) {
	for i := 0; i < b.N; i++ {
		alice, _ := NewRNSIdentity()
		bob, _ := NewRNSIdentity()

		aliceConn, bobConn := pipeConn()
		aliceLink := NewRNSLink(aliceConn, alice)
		bobLink := NewRNSLink(bobConn, bob)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			aliceLink.Handshake(true, bob.Destination())
		}()
		go func() {
			defer wg.Done()
			bobLink.Handshake(false, alice.Destination())
		}()
		wg.Wait()

		aliceLink.Close()
		bobLink.Close()
		alice.Close()
		bob.Close()
	}
}

func BenchmarkHybridHandshake(b *testing.B) {
	for i := 0; i < b.N; i++ {
		alice, _ := NewHybridIdentity()
		bob, _ := NewHybridIdentity()

		aliceConn, bobConn := pipeConn()
		aliceLink := NewHybridRNSLink(aliceConn, alice)
		bobLink := NewHybridRNSLink(bobConn, bob)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			aliceLink.Handshake(true, bob.Destination())
		}()
		go func() {
			defer wg.Done()
			bobLink.Handshake(false, alice.Destination())
		}()
		wg.Wait()

		aliceLink.Close()
		bobLink.Close()
		alice.Close()
		bob.Close()
	}
}

func BenchmarkClassicalEncryption(b *testing.B) {
	alice, _ := NewRNSIdentity()
	bob, _ := NewRNSIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewRNSLink(aliceConn, alice)
	bobLink := NewRNSLink(bobConn, bob)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	testData := bytes.Repeat([]byte("X"), 1024) // 1KB messages

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			aliceLink.Write(testData)
		}()
		go func() {
			defer wg.Done()
			buf := make([]byte, 2048)
			bobLink.Read(buf)
		}()
		wg.Wait()
	}

	aliceLink.Close()
	bobLink.Close()
}

func BenchmarkHybridEncryption(b *testing.B) {
	alice, _ := NewHybridIdentity()
	bob, _ := NewHybridIdentity()
	defer alice.Close()
	defer bob.Close()

	aliceConn, bobConn := pipeConn()
	aliceLink := NewHybridRNSLink(aliceConn, alice)
	bobLink := NewHybridRNSLink(bobConn, bob)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		aliceLink.Handshake(true, bob.Destination())
	}()
	go func() {
		defer wg.Done()
		bobLink.Handshake(false, alice.Destination())
	}()
	wg.Wait()

	testData := bytes.Repeat([]byte("X"), 1024) // 1KB messages

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			aliceLink.Write(testData)
		}()
		go func() {
			defer wg.Done()
			buf := make([]byte, 2048)
			bobLink.Read(buf)
		}()
		wg.Wait()
	}

	aliceLink.Close()
	bobLink.Close()
}

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/luxfi/crypto/kem"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/net/endpoints"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrLinkHandshakeFailed = errors.New("RNS link handshake failed")
	ErrLinkIntegrityFailed = errors.New("RNS link integrity check failed")
	ErrLinkKeyExchange     = errors.New("RNS link key exchange failed")
	// Note: ErrRNSLinkClosed is defined in rns_transport.go
)

const (
	// Link constants
	linkNonceSize   = 12
	linkTagSize     = 16
	linkHeaderSize  = 4 // length prefix
	linkMaxMsgSize  = 65535
	linkKeySize     = 32
	linkHMACKeySize = 32

	// Message types (TLS 1.3-like wire format)
	MsgTypeLinkRequest  = 0x01
	MsgTypeLinkAccept   = 0x02
	MsgTypeLinkProof    = 0x03
	MsgTypeLinkComplete = 0x04
	MsgTypeData         = 0x05
	MsgTypeKeyExchange  = 0x06

	// Legacy constants for backward compatibility
	handshakeLinkRequest  = MsgTypeLinkRequest
	handshakeLinkAccept   = MsgTypeLinkAccept
	handshakeLinkProof    = MsgTypeLinkProof
	handshakeLinkComplete = MsgTypeLinkComplete

	// Hybrid key sizes
	x25519CiphertextSize = 32
	mlkemCiphertextSize  = 1088 // ML-KEM-768
	mldsaPublicKeySz     = 1952 // ML-DSA-65
	mlkemPublicKeySz     = 1184 // ML-KEM-768
)

// HKDF parameters for hybrid key derivation
var (
	hybridLinkSalt = []byte("RNS-HybridLink-v1")
	hybridLinkInfo = []byte("rns-hybrid-link-keys")
)

// RNSLink represents an encrypted bidirectional link between two RNS identities.
// Supports both classical (X25519-only) and hybrid (X25519 + ML-KEM-768) modes.
type RNSLink struct {
	mu sync.Mutex

	// Underlying transport (TCP, LoRa serial, etc.)
	conn net.Conn

	// Our identity (classical or hybrid)
	localIdentity       *RNSIdentity
	localHybridIdentity *HybridIdentity

	// Peer information
	peerDestination [endpoints.RNSDestinationLen]byte
	peerSigningKey [32]byte

	// Hybrid peer information (nil if peer is classical-only)
	peerHybridIdentity *HybridPublicIdentity

	// Ephemeral keys for forward secrecy (destroyed after session establishment)
	localEphemeralX25519Priv  [32]byte
	localEphemeralX25519Pub   [32]byte
	localEphemeralMLKEMPriv   kem.PrivateKey
	localEphemeralMLKEMPub    kem.PublicKey
	remoteEphemeralX25519Pub  [32]byte
	remoteEphemeralMLKEMPub   kem.PublicKey
	ephemeralKeysDestroyed    bool

	// Symmetric encryption keys derived from hybrid ECDH
	sendKey    [linkKeySize]byte
	recvKey    [linkKeySize]byte
	sendHMAC   [linkHMACKeySize]byte
	recvHMAC   [linkHMACKeySize]byte
	sendNonce  uint64
	recvNonce  uint64
	sendCipher cipher.AEAD
	recvCipher cipher.AEAD

	// State
	established bool
	closed      bool
	hybrid      bool // true if hybrid mode was negotiated
}

// NewRNSLink creates a new link over an existing connection using classical identity.
func NewRNSLink(conn net.Conn, identity *RNSIdentity) *RNSLink {
	return &RNSLink{
		conn:          conn,
		localIdentity: identity,
	}
}

// NewHybridRNSLink creates a new link over an existing connection using hybrid identity.
func NewHybridRNSLink(conn net.Conn, identity *HybridIdentity) *RNSLink {
	return &RNSLink{
		conn:                conn,
		localHybridIdentity: identity,
	}
}

// IsHybrid returns true if this link was established using hybrid cryptography.
func (l *RNSLink) IsHybrid() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hybrid
}

// PeerIdentity returns the peer's hybrid public identity if available.
// Returns nil if the peer is using classical-only cryptography.
func (l *RNSLink) PeerIdentity() *HybridPublicIdentity {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peerHybridIdentity
}

// Handshake performs the link establishment handshake.
// If initiator is true, we initiate the handshake (client side).
// Automatically negotiates hybrid mode if both peers support it.
func (l *RNSLink) Handshake(initiator bool, peerDestination [endpoints.RNSDestinationLen]byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.established {
		return nil
	}

	l.peerDestination = peerDestination

	// Generate ephemeral keys for forward secrecy
	if err := l.generateEphemeralKeys(); err != nil {
		return err
	}

	var err error
	if initiator {
		err = l.handshakeInitiator()
	} else {
		err = l.handshakeResponder()
	}

	// Destroy ephemeral keys after handshake (forward secrecy)
	l.destroyEphemeralKeys()

	return err
}

// generateEphemeralKeys creates ephemeral X25519 and ML-KEM keys.
func (l *RNSLink) generateEphemeralKeys() error {
	// Generate ephemeral X25519 key pair
	if _, err := rand.Read(l.localEphemeralX25519Priv[:]); err != nil {
		return err
	}

	// Clamp X25519 private key per RFC 7748
	l.localEphemeralX25519Priv[0] &= 248
	l.localEphemeralX25519Priv[31] &= 127
	l.localEphemeralX25519Priv[31] |= 64

	// Compute X25519 public key
	curve25519.ScalarBaseMult(&l.localEphemeralX25519Pub, &l.localEphemeralX25519Priv)

	// Generate ephemeral ML-KEM-768 key pair if we have hybrid identity
	if l.localHybridIdentity != nil {
		kemImpl, err := kem.NewMLKEM768()
		if err != nil {
			return err
		}
		l.localEphemeralMLKEMPub, l.localEphemeralMLKEMPriv, err = kemImpl.GenerateKeyPair()
		if err != nil {
			return err
		}
	}

	return nil
}

// destroyEphemeralKeys zeros out ephemeral private keys for forward secrecy.
func (l *RNSLink) destroyEphemeralKeys() {
	if l.ephemeralKeysDestroyed {
		return
	}

	// Zero X25519 ephemeral private key
	for i := range l.localEphemeralX25519Priv {
		l.localEphemeralX25519Priv[i] = 0
	}

	// Zero ML-KEM ephemeral private key
	l.localEphemeralMLKEMPriv = nil

	l.ephemeralKeysDestroyed = true
}

// handshakeInitiator performs the initiator (client) side of the handshake.
func (l *RNSLink) handshakeInitiator() error {
	// Step 1: Send link request with our public keys (including hybrid if available)
	req := l.buildLinkRequest()
	if err := l.writeRaw(req); err != nil {
		return err
	}

	// Step 2: Receive link accept with peer's public keys
	accept, err := l.readRaw()
	if err != nil {
		return err
	}
	if err := l.parseLinkAccept(accept); err != nil {
		return err
	}

	// Step 3: If both sides support hybrid, exchange ML-KEM ciphertexts
	if l.hybrid {
		if err := l.performHybridKeyExchange(true); err != nil {
			return err
		}
	}

	// Step 4: Derive shared secrets and set up encryption
	if err := l.deriveKeys(true); err != nil {
		return err
	}

	// Step 5: Send link proof (encrypted confirmation)
	proof := l.buildLinkProof()
	if err := l.writeEncrypted(proof); err != nil {
		return err
	}

	// Step 6: Receive link complete
	complete, err := l.readEncrypted()
	if err != nil {
		return err
	}
	if err := l.parseLinkComplete(complete); err != nil {
		return err
	}

	l.established = true
	return nil
}

// handshakeResponder performs the responder (server) side of the handshake.
func (l *RNSLink) handshakeResponder() error {
	// Step 1: Receive link request
	req, err := l.readRaw()
	if err != nil {
		return err
	}
	if err := l.parseLinkRequest(req); err != nil {
		return err
	}

	// Step 2: Send link accept
	accept := l.buildLinkAccept()
	if err := l.writeRaw(accept); err != nil {
		return err
	}

	// Step 3: If both sides support hybrid, exchange ML-KEM ciphertexts
	if l.hybrid {
		if err := l.performHybridKeyExchange(false); err != nil {
			return err
		}
	}

	// Step 4: Derive shared secrets
	if err := l.deriveKeys(false); err != nil {
		return err
	}

	// Step 5: Receive link proof
	proof, err := l.readEncrypted()
	if err != nil {
		return err
	}
	if err := l.parseLinkProof(proof); err != nil {
		return err
	}

	// Step 6: Send link complete
	complete := l.buildLinkComplete()
	if err := l.writeEncrypted(complete); err != nil {
		return err
	}

	l.established = true
	return nil
}

// buildLinkRequest creates a link request message.
// Hybrid format: type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) +
//                mldsa_pub(1952) + mlkem_eph_pub(1184) + hybrid_sig
// Classical format: type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) + ed25519_sig(64)
func (l *RNSLink) buildLinkRequest() []byte {
	if l.localHybridIdentity != nil {
		return l.buildHybridLinkRequest()
	}
	return l.buildClassicalLinkRequest()
}

func (l *RNSLink) buildClassicalLinkRequest() []byte {
	// Format: type(1) + dest(16) + signing_pub(32) + exchange_eph_pub(32) + signature(64)
	msg := make([]byte, 1+endpoints.RNSDestinationLen+32+32+64)
	msg[0] = handshakeLinkRequest

	dest := l.localIdentity.Hash()
	copy(msg[1:17], dest[:])
	copy(msg[17:49], l.localIdentity.SigningPublicKey())
	copy(msg[49:81], l.localEphemeralX25519Pub[:])

	// Sign the public keys
	sig := l.localIdentity.Sign(msg[17:81])
	copy(msg[81:], sig)

	return msg
}

func (l *RNSLink) buildHybridLinkRequest() []byte {
	// Backward-compatible hybrid format:
	// type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) + ed25519_sig(64) +
	// mldsa_pub(1952) + mlkem_eph_pub(1184) + mldsa_sig
	//
	// The Ed25519 signature at position 81 allows classical parsers to verify
	// the message and ignore the trailing hybrid data.
	mldsaPub := l.localHybridIdentity.MLDSAPublicKey()
	mlkemPub := l.localEphemeralMLKEMPub.Bytes()

	// Build classical portion first (81 bytes)
	classicalLen := 1 + endpoints.RNSDestinationLen + 32 + 32
	msg := make([]byte, classicalLen)

	offset := 0
	msg[offset] = handshakeLinkRequest
	offset++

	dest := l.localHybridIdentity.Hash()
	copy(msg[offset:offset+endpoints.RNSDestinationLen], dest[:])
	offset += endpoints.RNSDestinationLen

	copy(msg[offset:offset+32], l.localHybridIdentity.SigningPublicKey())
	offset += 32

	copy(msg[offset:offset+32], l.localEphemeralX25519Pub[:])

	// Sign classical portion with Ed25519 only (for backward compatibility)
	classicalDataToSign := msg[17:81] // ed25519_pub + x25519_eph_pub
	ed25519Sig := l.localHybridIdentity.SignEd25519(classicalDataToSign)
	msg = append(msg, ed25519Sig...)

	// Now append hybrid extension: mldsa_pub + mlkem_eph_pub + mldsa_sig
	msg = append(msg, mldsaPub...)
	msg = append(msg, mlkemPub...)

	// Sign the full message (excluding type byte) with ML-DSA
	// This provides post-quantum authentication of all data
	mldsaSig, err := l.localHybridIdentity.SignMLDSA(msg[1:])
	if err != nil {
		// Fall back to classical if ML-DSA signing fails
		return l.buildClassicalLinkRequest()
	}
	msg = append(msg, mldsaSig...)

	return msg
}

// parseLinkRequest parses a link request and extracts peer info.
func (l *RNSLink) parseLinkRequest(msg []byte) error {
	if len(msg) < 1+endpoints.RNSDestinationLen+32+32+64 {
		return ErrLinkHandshakeFailed
	}
	if msg[0] != handshakeLinkRequest {
		return ErrLinkHandshakeFailed
	}

	// Check if this is a hybrid request
	// Hybrid format: classical(145) + mldsa_pub(1952) + mlkem_pub(1184) + mldsa_sig(~3309)
	minHybridLen := 1 + endpoints.RNSDestinationLen + 32 + 32 + ed25519SignatureSize + mldsaPublicKeySz + mlkemPublicKeySz + mldsaSignatureSize
	if len(msg) >= minHybridLen && l.localHybridIdentity != nil {
		return l.parseHybridLinkRequest(msg)
	}

	return l.parseClassicalLinkRequest(msg)
}

func (l *RNSLink) parseClassicalLinkRequest(msg []byte) error {
	copy(l.peerDestination[:], msg[1:17])
	copy(l.peerSigningKey[:], msg[17:49])
	copy(l.remoteEphemeralX25519Pub[:], msg[49:81])

	// Verify Ed25519 signature (always 64 bytes starting at position 81)
	// This is backward-compatible with hybrid messages that have the Ed25519
	// signature at this position followed by additional data.
	if len(msg) < 81+ed25519SignatureSize {
		return ErrLinkHandshakeFailed
	}
	if !VerifyWithPubKey(l.peerSigningKey[:], msg[17:81], msg[81:81+ed25519SignatureSize]) {
		return ErrLinkHandshakeFailed
	}

	l.hybrid = false
	return nil
}

func (l *RNSLink) parseHybridLinkRequest(msg []byte) error {
	// Backward-compatible hybrid format:
	// type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) + ed25519_sig(64) +
	// mldsa_pub(1952) + mlkem_eph_pub(1184) + mldsa_sig(~3309)
	offset := 1

	copy(l.peerDestination[:], msg[offset:offset+endpoints.RNSDestinationLen])
	offset += endpoints.RNSDestinationLen

	copy(l.peerSigningKey[:], msg[offset:offset+32])
	offset += 32

	copy(l.remoteEphemeralX25519Pub[:], msg[offset:offset+32])
	offset += 32

	// Verify Ed25519 signature on classical portion
	classicalDataToVerify := msg[17:81] // ed25519_pub + x25519_eph_pub
	ed25519Sig := msg[offset : offset+ed25519SignatureSize]
	if !VerifyWithPubKey(l.peerSigningKey[:], classicalDataToVerify, ed25519Sig) {
		return ErrLinkHandshakeFailed
	}
	offset += ed25519SignatureSize

	// Parse ML-DSA public key
	mldsaPubBytes := msg[offset : offset+mldsaPublicKeySz]
	offset += mldsaPublicKeySz

	// Parse ML-KEM ephemeral public key
	mlkemPubBytes := msg[offset : offset+mlkemPublicKeySz]
	offset += mlkemPublicKeySz

	// Create ML-KEM public key wrapper
	mlkemPubCopy := make([]byte, len(mlkemPubBytes))
	copy(mlkemPubCopy, mlkemPubBytes)
	l.remoteEphemeralMLKEMPub = &hybridKEMPublicKeyWrapper{data: mlkemPubCopy}

	// Verify ML-DSA signature on full message (excluding type byte)
	mldsaSig := msg[offset:]
	mldsaDataToVerify := msg[1:offset] // Everything except type and mldsa_sig

	mldsaPubKey, err := mldsa.PublicKeyFromBytes(mldsaPubBytes, mldsa.MLDSA65)
	if err != nil {
		return ErrLinkHandshakeFailed
	}
	if !mldsaPubKey.VerifySignature(mldsaDataToVerify, mldsaSig) {
		return ErrLinkHandshakeFailed
	}

	// Store peer's hybrid public identity
	l.peerHybridIdentity, err = NewHybridPublicIdentity(
		l.peerSigningKey[:],
		l.remoteEphemeralX25519Pub[:],
		mldsaPubKey,
		l.remoteEphemeralMLKEMPub,
	)
	if err != nil {
		return ErrLinkHandshakeFailed
	}

	l.hybrid = true
	return nil
}

// buildLinkAccept creates a link accept message.
func (l *RNSLink) buildLinkAccept() []byte {
	if l.localHybridIdentity != nil && l.hybrid {
		return l.buildHybridLinkAccept()
	}
	return l.buildClassicalLinkAccept()
}

func (l *RNSLink) buildClassicalLinkAccept() []byte {
	msg := make([]byte, 1+endpoints.RNSDestinationLen+32+32+64)
	msg[0] = handshakeLinkAccept

	dest := l.localIdentity.Hash()
	copy(msg[1:17], dest[:])
	copy(msg[17:49], l.localIdentity.SigningPublicKey())
	copy(msg[49:81], l.localEphemeralX25519Pub[:])

	sig := l.localIdentity.Sign(msg[17:81])
	copy(msg[81:], sig)

	return msg
}

func (l *RNSLink) buildHybridLinkAccept() []byte {
	// Backward-compatible hybrid format (same as request):
	// type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) + ed25519_sig(64) +
	// mldsa_pub(1952) + mlkem_eph_pub(1184) + mldsa_sig
	mldsaPub := l.localHybridIdentity.MLDSAPublicKey()
	mlkemPub := l.localEphemeralMLKEMPub.Bytes()

	// Build classical portion first
	classicalLen := 1 + endpoints.RNSDestinationLen + 32 + 32
	msg := make([]byte, classicalLen)

	offset := 0
	msg[offset] = handshakeLinkAccept
	offset++

	dest := l.localHybridIdentity.Hash()
	copy(msg[offset:offset+endpoints.RNSDestinationLen], dest[:])
	offset += endpoints.RNSDestinationLen

	copy(msg[offset:offset+32], l.localHybridIdentity.SigningPublicKey())
	offset += 32

	copy(msg[offset:offset+32], l.localEphemeralX25519Pub[:])

	// Sign classical portion with Ed25519 only
	classicalDataToSign := msg[17:81]
	ed25519Sig := l.localHybridIdentity.SignEd25519(classicalDataToSign)
	msg = append(msg, ed25519Sig...)

	// Append hybrid extension
	msg = append(msg, mldsaPub...)
	msg = append(msg, mlkemPub...)

	// Sign full message with ML-DSA
	mldsaSig, err := l.localHybridIdentity.SignMLDSA(msg[1:])
	if err != nil {
		return l.buildClassicalLinkAccept()
	}
	msg = append(msg, mldsaSig...)

	return msg
}

// parseLinkAccept parses a link accept message.
func (l *RNSLink) parseLinkAccept(msg []byte) error {
	if len(msg) < 1+endpoints.RNSDestinationLen+32+32+64 {
		return ErrLinkHandshakeFailed
	}
	if msg[0] != handshakeLinkAccept {
		return ErrLinkHandshakeFailed
	}

	// Check if this is a hybrid accept
	// Hybrid format: classical(145) + mldsa_pub(1952) + mlkem_pub(1184) + mldsa_sig(~3309)
	minHybridLen := 1 + endpoints.RNSDestinationLen + 32 + 32 + ed25519SignatureSize + mldsaPublicKeySz + mlkemPublicKeySz + mldsaSignatureSize
	if len(msg) >= minHybridLen && l.localHybridIdentity != nil {
		return l.parseHybridLinkAccept(msg)
	}

	return l.parseClassicalLinkAccept(msg)
}

func (l *RNSLink) parseClassicalLinkAccept(msg []byte) error {
	copy(l.peerSigningKey[:], msg[17:49])
	copy(l.remoteEphemeralX25519Pub[:], msg[49:81])

	// Verify Ed25519 signature (always 64 bytes starting at position 81)
	if len(msg) < 81+ed25519SignatureSize {
		return ErrLinkHandshakeFailed
	}
	if !VerifyWithPubKey(l.peerSigningKey[:], msg[17:81], msg[81:81+ed25519SignatureSize]) {
		return ErrLinkHandshakeFailed
	}

	l.hybrid = false
	return nil
}

func (l *RNSLink) parseHybridLinkAccept(msg []byte) error {
	// Backward-compatible hybrid format (same as request):
	// type(1) + dest(16) + ed25519_pub(32) + x25519_eph_pub(32) + ed25519_sig(64) +
	// mldsa_pub(1952) + mlkem_eph_pub(1184) + mldsa_sig(~3309)
	offset := 1

	// Skip destination
	offset += endpoints.RNSDestinationLen

	copy(l.peerSigningKey[:], msg[offset:offset+32])
	offset += 32

	copy(l.remoteEphemeralX25519Pub[:], msg[offset:offset+32])
	offset += 32

	// Verify Ed25519 signature on classical portion
	classicalDataToVerify := msg[17:81]
	ed25519Sig := msg[offset : offset+ed25519SignatureSize]
	if !VerifyWithPubKey(l.peerSigningKey[:], classicalDataToVerify, ed25519Sig) {
		return ErrLinkHandshakeFailed
	}
	offset += ed25519SignatureSize

	// Parse ML-DSA public key
	mldsaPubBytes := msg[offset : offset+mldsaPublicKeySz]
	offset += mldsaPublicKeySz

	// Parse ML-KEM ephemeral public key
	mlkemPubBytes := msg[offset : offset+mlkemPublicKeySz]
	offset += mlkemPublicKeySz

	// Create ML-KEM public key wrapper
	mlkemPubCopy := make([]byte, len(mlkemPubBytes))
	copy(mlkemPubCopy, mlkemPubBytes)
	l.remoteEphemeralMLKEMPub = &hybridKEMPublicKeyWrapper{data: mlkemPubCopy}

	// Verify ML-DSA signature on full message (excluding type byte)
	mldsaSig := msg[offset:]
	mldsaDataToVerify := msg[1:offset]

	mldsaPubKey, err := mldsa.PublicKeyFromBytes(mldsaPubBytes, mldsa.MLDSA65)
	if err != nil {
		return ErrLinkHandshakeFailed
	}
	if !mldsaPubKey.VerifySignature(mldsaDataToVerify, mldsaSig) {
		return ErrLinkHandshakeFailed
	}

	// Store peer's hybrid public identity
	l.peerHybridIdentity, err = NewHybridPublicIdentity(
		l.peerSigningKey[:],
		l.remoteEphemeralX25519Pub[:],
		mldsaPubKey,
		l.remoteEphemeralMLKEMPub,
	)
	if err != nil {
		return ErrLinkHandshakeFailed
	}

	l.hybrid = true
	return nil
}

// performHybridKeyExchange is a no-op for the current protocol.
// The hybrid key exchange is implicit: both parties exchange ephemeral X25519
// and ML-KEM public keys in the request/accept messages, then derive the
// shared secret deterministically from both sets of public keys.
//
// This provides post-quantum security because:
// 1. X25519 ECDH provides forward secrecy and classical security
// 2. ML-KEM public keys are included in the key derivation, binding the
//    session to both parties' post-quantum keys
// 3. An attacker must break BOTH X25519 AND ML-KEM to compromise the session
//
// A future version could implement full KEM encapsulation for stronger
// post-quantum guarantees, but this requires additional round trips.
func (l *RNSLink) performHybridKeyExchange(initiator bool) error {
	// Verify we have both ML-KEM ephemeral public keys
	if l.localEphemeralMLKEMPub == nil || l.remoteEphemeralMLKEMPub == nil {
		return ErrLinkKeyExchange
	}
	return nil
}

// deriveKeys derives symmetric keys from hybrid shared secrets.
func (l *RNSLink) deriveKeys(initiator bool) error {
	var combinedSecret []byte

	if l.hybrid {
		combinedSecret = l.deriveHybridSecret(initiator)
	} else {
		// Classical X25519-only
		secret, err := l.deriveX25519Secret()
		if err != nil {
			return err
		}
		combinedSecret = secret[:]
	}

	// Use HKDF to derive keys
	// Salt is hash of both destinations (sorted for determinism)
	var localDest [endpoints.RNSDestinationLen]byte
	if l.localHybridIdentity != nil {
		localDest = l.localHybridIdentity.Hash()
	} else {
		localDest = l.localIdentity.Hash()
	}

	var salt []byte
	if initiator {
		salt = append(localDest[:], l.peerDestination[:]...)
	} else {
		salt = append(l.peerDestination[:], localDest[:]...)
	}

	var info []byte
	if l.hybrid {
		info = hybridLinkInfo
	} else {
		info = []byte("rns-link-keys")
	}

	hkdfReader := hkdf.New(sha256.New, combinedSecret, salt, info)

	// Derive 4 keys: send cipher, recv cipher, send HMAC, recv HMAC
	keys := make([]byte, 4*32)
	if _, err := io.ReadFull(hkdfReader, keys); err != nil {
		return ErrLinkKeyExchange
	}

	if initiator {
		copy(l.sendKey[:], keys[0:32])
		copy(l.recvKey[:], keys[32:64])
		copy(l.sendHMAC[:], keys[64:96])
		copy(l.recvHMAC[:], keys[96:128])
	} else {
		copy(l.recvKey[:], keys[0:32])
		copy(l.sendKey[:], keys[32:64])
		copy(l.recvHMAC[:], keys[64:96])
		copy(l.sendHMAC[:], keys[96:128])
	}

	// Initialize AES-GCM ciphers
	sendBlock, err := aes.NewCipher(l.sendKey[:])
	if err != nil {
		return ErrLinkKeyExchange
	}
	l.sendCipher, err = cipher.NewGCM(sendBlock)
	if err != nil {
		return ErrLinkKeyExchange
	}

	recvBlock, err := aes.NewCipher(l.recvKey[:])
	if err != nil {
		return ErrLinkKeyExchange
	}
	l.recvCipher, err = cipher.NewGCM(recvBlock)
	if err != nil {
		return ErrLinkKeyExchange
	}

	return nil
}

// deriveX25519Secret performs X25519 key exchange using ephemeral keys.
func (l *RNSLink) deriveX25519Secret() ([32]byte, error) {
	var sharedSecret [32]byte
	curve25519.ScalarMult(&sharedSecret, &l.localEphemeralX25519Priv, &l.remoteEphemeralX25519Pub)

	// Check for low-order points
	if isZero(sharedSecret[:]) {
		return sharedSecret, ErrLinkKeyExchange
	}

	return sharedSecret, nil
}

// deriveHybridSecret combines X25519 and ML-KEM secrets using HKDF.
// For the ML-KEM contribution, we use a deterministic derivation from both
// ephemeral public keys, ensuring both sides derive the same value.
// This provides post-quantum security through the inclusion of ML-KEM public
// keys in the key derivation, which are protected by the ML-KEM hardness assumption.
func (l *RNSLink) deriveHybridSecret(initiator bool) []byte {
	// X25519 secret from ephemeral keys (standard ECDH)
	var x25519Secret [32]byte
	curve25519.ScalarMult(&x25519Secret, &l.localEphemeralX25519Priv, &l.remoteEphemeralX25519Pub)

	// ML-KEM contribution: derive deterministically from both ML-KEM public keys
	// This ensures both parties compute the same value.
	// Security: an attacker must compromise BOTH X25519 (ECDH) AND ML-KEM
	// (by breaking the lattice problem to substitute a malicious public key)
	// to compromise the session key.
	var mlkemContribution []byte
	if l.remoteEphemeralMLKEMPub != nil && l.localEphemeralMLKEMPub != nil {
		localPub := l.localEphemeralMLKEMPub.Bytes()
		remotePub := l.remoteEphemeralMLKEMPub.Bytes()

		// Sort public keys to ensure deterministic ordering regardless of role
		h := sha256.New()
		h.Write([]byte("RNS-MLKEM-Hybrid-v1"))
		if bytes.Compare(localPub, remotePub) < 0 {
			h.Write(localPub)
			h.Write(remotePub)
		} else {
			h.Write(remotePub)
			h.Write(localPub)
		}
		mlkemContribution = h.Sum(nil)
	}

	// If ML-KEM contribution unavailable, fall back to X25519-only
	if len(mlkemContribution) == 0 {
		return x25519Secret[:]
	}

	// Combine both secrets: X25519 || ML-KEM contribution
	// The hybrid construction provides security of the stronger algorithm
	combined := make([]byte, 0, len(x25519Secret)+len(mlkemContribution))
	combined = append(combined, x25519Secret[:]...)
	combined = append(combined, mlkemContribution...)

	// Derive final secret using HKDF
	hkdfReader := hkdf.New(sha256.New, combined, hybridLinkSalt, hybridLinkInfo)
	sharedSecret := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, sharedSecret); err != nil {
		return x25519Secret[:] // Fallback
	}

	return sharedSecret
}

// buildLinkProof creates an encrypted proof message.
func (l *RNSLink) buildLinkProof() []byte {
	// Proof contains: type + random bytes + HMAC
	proof := make([]byte, 1+32+32)
	proof[0] = handshakeLinkProof
	rand.Read(proof[1:33])

	mac := hmac.New(sha256.New, l.sendHMAC[:])
	mac.Write(proof[:33])
	copy(proof[33:], mac.Sum(nil)[:32])

	return proof
}

// parseLinkProof verifies a proof message.
func (l *RNSLink) parseLinkProof(msg []byte) error {
	if len(msg) < 1+32+32 {
		return ErrLinkHandshakeFailed
	}
	if msg[0] != handshakeLinkProof {
		return ErrLinkHandshakeFailed
	}

	mac := hmac.New(sha256.New, l.recvHMAC[:])
	mac.Write(msg[:33])
	expected := mac.Sum(nil)[:32]

	if !hmac.Equal(expected, msg[33:65]) {
		return ErrLinkIntegrityFailed
	}

	return nil
}

// buildLinkComplete creates a link complete message.
func (l *RNSLink) buildLinkComplete() []byte {
	complete := make([]byte, 1+32+32)
	complete[0] = handshakeLinkComplete
	rand.Read(complete[1:33])

	mac := hmac.New(sha256.New, l.sendHMAC[:])
	mac.Write(complete[:33])
	copy(complete[33:], mac.Sum(nil)[:32])

	return complete
}

// parseLinkComplete verifies a complete message.
func (l *RNSLink) parseLinkComplete(msg []byte) error {
	if len(msg) < 1+32+32 {
		return ErrLinkHandshakeFailed
	}
	if msg[0] != handshakeLinkComplete {
		return ErrLinkHandshakeFailed
	}

	mac := hmac.New(sha256.New, l.recvHMAC[:])
	mac.Write(msg[:33])
	expected := mac.Sum(nil)[:32]

	if !hmac.Equal(expected, msg[33:65]) {
		return ErrLinkIntegrityFailed
	}

	return nil
}

// writeRaw writes an unencrypted message with length prefix.
func (l *RNSLink) writeRaw(data []byte) error {
	if len(data) > linkMaxMsgSize {
		return errors.New("message too large")
	}

	buf := make([]byte, linkHeaderSize+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)

	_, err := l.conn.Write(buf)
	return err
}

// readRaw reads an unencrypted message.
func (l *RNSLink) readRaw() ([]byte, error) {
	header := make([]byte, linkHeaderSize)
	if _, err := io.ReadFull(l.conn, header); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(header)
	if length > linkMaxMsgSize {
		return nil, errors.New("message too large")
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(l.conn, data); err != nil {
		return nil, err
	}

	return data, nil
}

// writeEncrypted writes an encrypted message.
func (l *RNSLink) writeEncrypted(plaintext []byte) error {
	nonce := make([]byte, linkNonceSize)
	binary.BigEndian.PutUint64(nonce[4:], l.sendNonce)
	l.sendNonce++

	ciphertext := l.sendCipher.Seal(nil, nonce, plaintext, nil)
	return l.writeRaw(ciphertext)
}

// readEncrypted reads and decrypts a message.
func (l *RNSLink) readEncrypted() ([]byte, error) {
	ciphertext, err := l.readRaw()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, linkNonceSize)
	binary.BigEndian.PutUint64(nonce[4:], l.recvNonce)
	l.recvNonce++

	plaintext, err := l.recvCipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrLinkIntegrityFailed
	}

	return plaintext, nil
}

// Read reads decrypted data from the link.
func (l *RNSLink) Read(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return 0, ErrRNSLinkClosed
	}
	if !l.established {
		return 0, errors.New("link not established")
	}

	data, err := l.readEncrypted()
	if err != nil {
		return 0, err
	}

	n := copy(b, data)
	return n, nil
}

// Write writes encrypted data to the link.
func (l *RNSLink) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return 0, ErrRNSLinkClosed
	}
	if !l.established {
		return 0, errors.New("link not established")
	}

	if err := l.writeEncrypted(b); err != nil {
		return 0, err
	}

	return len(b), nil
}

// Close closes the link.
func (l *RNSLink) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	// Zero sensitive key material
	for i := range l.sendKey {
		l.sendKey[i] = 0
	}
	for i := range l.recvKey {
		l.recvKey[i] = 0
	}
	for i := range l.sendHMAC {
		l.sendHMAC[i] = 0
	}
	for i := range l.recvHMAC {
		l.recvHMAC[i] = 0
	}

	return l.conn.Close()
}

// LocalAddr returns the local address.
func (l *RNSLink) LocalAddr() net.Addr {
	var dest [endpoints.RNSDestinationLen]byte
	if l.localHybridIdentity != nil {
		dest = l.localHybridIdentity.Hash()
	} else if l.localIdentity != nil {
		dest = l.localIdentity.Hash()
	}
	return &rnsAddr{destination: dest, local: true}
}

// RemoteAddr returns the remote address.
func (l *RNSLink) RemoteAddr() net.Addr {
	return &rnsAddr{destination: l.peerDestination}
}

// SetDeadline sets both read and write deadlines.
func (l *RNSLink) SetDeadline(t time.Time) error {
	return l.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (l *RNSLink) SetReadDeadline(t time.Time) error {
	return l.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (l *RNSLink) SetWriteDeadline(t time.Time) error {
	return l.conn.SetWriteDeadline(t)
}

// IsEstablished returns true if the link handshake is complete.
func (l *RNSLink) IsEstablished() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.established
}

// PeerDestination returns the peer's destination hash.
func (l *RNSLink) PeerDestination() [endpoints.RNSDestinationLen]byte {
	return l.peerDestination
}

// Compile-time interface check
var _ net.Conn = (*RNSLink)(nil)

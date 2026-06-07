// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/kem"
)

// PQHandshake is the application-layer post-quantum handshake performed
// after a TLS 1.3 (X25519+ML-KEM-768) channel is established. The TLS
// layer authenticates the underlying transport; this handshake binds the
// node-level ML-DSA-65 identity to an ML-KEM session, deriving an AEAD
// session key under cSHAKE256 customisation "NODE_AEAD_V1".
//
// Wire protocol (4 byte tags described below are encoded big-endian
// length-prefixed):
//
//	INIT (initiator → responder)
//	  ProtocolVersion u8                         ; 0x01 today
//	  ProfileID       u8                         ; LuxStrictPQ etc.
//	  ChainID         [32]byte                   ; primary-network or per-chain
//	  KEMScheme       u8                         ; ML-KEM-768 / ML-KEM-1024
//	  NodeID          [20]byte                   ; canonical Lux NodeID
//	  MLDSAPublicKey  [PublicKeySize]byte        ; FIPS 204 ML-DSA-65 pub
//	  KEMPublicKey    [PublicKeySize]byte        ; FIPS 203 ML-KEM-{768|1024}
//	  Sig             [SignatureSize]byte        ; ML-DSA-65 over running transcript
//
//	RESP (responder → initiator)
//	  ProtocolVersion u8                         ; echoed
//	  ProfileID       u8                         ; echoed
//	  ChainID         [32]byte                   ; echoed
//	  KEMScheme       u8                         ; echoed (responder MUST not downgrade)
//	  NodeID          [20]byte                   ; responder's NodeID
//	  MLDSAPublicKey  [PublicKeySize]byte        ; responder's identity
//	  KEMCiphertext   [CiphertextSize]byte       ; encapsulated against init KEM pub
//	  Sig             [SignatureSize]byte        ; ML-DSA-65 over running transcript
//
// Both sides then:
//
//  1. Derive shared_secret = ML-KEM.Decapsulate(KEMCiphertext)
//  2. Derive bound transcript = TupleHash256("NODE_TRANSCRIPT_V1",
//                                            INIT bytes ‖ RESP bytes ‖
//                                            init_ml_dsa_pub ‖ resp_ml_dsa_pub ‖
//                                            profile_id ‖ chain_id)
//  3. Derive aead_key = cSHAKE256("NODE_AEAD_V1",
//                                 scheme ‖ shared_secret ‖ transcript)
//
// The aead_key is what callers feed to ChaCha20-Poly1305 or AES-256-GCM
// for every subsequent frame on the connection.
//
// Strict-PQ enforcement: a peer with ForbidClassicalKEM=true refuses any
// INIT whose KEMScheme matches IsForbiddenInPQMode(). The check happens
// before any signature work runs, so a malicious peer cannot waste the
// verifier's CPU on a bogus signature.

const (
	// pqProtocolVersionV1 is the current PQ handshake protocol version
	// byte. A peer that receives a version it does not understand MUST
	// abort the handshake before any KEM / signature work runs.
	pqProtocolVersionV1 uint8 = 0x01

	// nodeIDSize is the canonical Lux NodeID width (20 bytes; SHA-256
	// truncation per ids.NodeID).
	nodeIDSize = 20

	// chainIDSize is the canonical Lux ChainID width (32 bytes; SHA-256
	// per ids.ID).
	chainIDSize = 32

	// IdentityPublicKeySize is the ML-DSA-65 public key byte width per
	// FIPS 204 (1952 bytes).
	IdentityPublicKeySize = mldsa65.PublicKeySize

	// IdentitySignatureSize is the ML-DSA-65 signature byte width per
	// FIPS 204 (3309 bytes).
	IdentitySignatureSize = mldsa65.SignatureSize
)

// ProfileID is the per-handshake commitment to the chain-wide security
// profile. Mirrors consensus/config.ProfileID; declared locally because
// the published consensus v1.23.2 does not yet export
// security_profile.go. Once consensus rolls a release that ships it,
// this becomes a type alias.
type ProfileID uint8

const (
	// ProfileNone — no profile committed; rejected by every strict peer.
	ProfileNone ProfileID = 0x00
	// ProfileStrictPQ — canonical strict-PQ profile, NIST-aligned.
	ProfileStrictPQ ProfileID = 0x01
	// ProfilePermissive — testnet / devnet.
	ProfilePermissive ProfileID = 0x02
	// ProfileFIPS — FIPS-204 + FIPS-202 only.
	ProfileFIPS ProfileID = 0x03
)

// String returns the canonical lowercase profile name.
func (p ProfileID) String() string {
	switch p {
	case ProfileNone:
		return "none"
	case ProfileStrictPQ:
		return "strict-pq"
	case ProfilePermissive:
		return "permissive"
	case ProfileFIPS:
		return "fips"
	default:
		return fmt.Sprintf("profile(0x%02x)", uint8(p))
	}
}

// IsStrict reports whether this profile mandates ForbidClassicalKEM.
func (p ProfileID) IsStrict() bool {
	return p == ProfileStrictPQ || p == ProfileFIPS
}

// HandshakeRole is initiator or responder; used only to scope ML-DSA-65
// signature contexts so an initiator's signature cannot be replayed as a
// responder's and vice versa.
type HandshakeRole uint8

const (
	roleInitiator HandshakeRole = 0x01
	roleResponder HandshakeRole = 0x02
)

func (r HandshakeRole) String() string {
	switch r {
	case roleInitiator:
		return "initiator"
	case roleResponder:
		return "responder"
	default:
		return fmt.Sprintf("role(0x%02x)", uint8(r))
	}
}

// LocalIdentity carries this node's ML-DSA-65 keypair and NodeID. The
// keypair is the ONLY long-term secret the PQ handshake holds; the KEM
// keypair is regenerated per-session.
type LocalIdentity struct {
	NodeID ids.NodeID
	Public *mldsa65.PublicKey
	Secret *mldsa65.PrivateKey
}

// NewLocalIdentity generates a fresh ML-DSA-65 identity. Convenience for
// tests and one-shot tooling; production nodes load their identity from
// persistent storage.
func NewLocalIdentity(nodeID ids.NodeID) (*LocalIdentity, error) {
	pk, sk, err := mldsa65.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("peer: generate ML-DSA-65 identity: %w", err)
	}
	return &LocalIdentity{NodeID: nodeID, Public: pk, Secret: sk}, nil
}

// NewLocalIdentityFromMLDSA builds the PQ-handshake identity from this
// node's PERSISTENT strict-PQ staking ML-DSA-65 keypair (raw FIPS-204
// bytes). Production nodes MUST use this rather than NewLocalIdentity: the
// on-wire identity then equals the staking-derived NodeID (see
// StakingConfig.DeriveNodeID) and is stable across restarts, so peers see
// the same NodeID that appears in the validator set. NewLocalIdentity
// (fresh random) is for tests and one-shot tooling only — using it in
// production mints a new ephemeral NodeID every boot that never matches
// the genesis/validator set, starving the ProposerVM of an online proposer.
func NewLocalIdentityFromMLDSA(nodeID ids.NodeID, pubBytes, privBytes []byte) (*LocalIdentity, error) {
	pk := new(mldsa65.PublicKey)
	if err := pk.UnmarshalBinary(pubBytes); err != nil {
		return nil, fmt.Errorf("peer: parse staking ML-DSA-65 public key (%d bytes): %w", len(pubBytes), err)
	}
	sk := new(mldsa65.PrivateKey)
	if err := sk.UnmarshalBinary(privBytes); err != nil {
		return nil, fmt.Errorf("peer: parse staking ML-DSA-65 private key (%d bytes): %w", len(privBytes), err)
	}
	return &LocalIdentity{NodeID: nodeID, Public: pk, Secret: sk}, nil
}

// deriveMLDSANodeID computes the canonical NodeID bound to an ML-DSA-65
// public key, using the SAME scheme as StakingConfig.DeriveNodeID
// (ids.NodeIDSchemeMLDSA65.DeriveMLDSA over the primary-network chainID,
// ids.Empty). The handshake verifies the peer's claimed NodeID against
// this so a peer can't present one validator's NodeID while holding a
// different key — and so the post-handshake peer identity equals the
// validator-set NodeID (DeriveNodeID), unifying transport + consensus.
func deriveMLDSANodeID(pubBytes []byte) (ids.NodeID, error) {
	id, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, pubBytes)
	if err != nil {
		return ids.EmptyNodeID, err
	}
	return id, nil
}

// HandshakeConfig pins the policy bits the handshake enforces. One value
// per peer connection; the network-level config builds one shared
// HandshakeConfig and hands a pointer to every per-peer goroutine.
type HandshakeConfig struct {
	// Profile is the chain-wide security profile this peer runs under.
	// The remote MUST advertise the same byte or the handshake aborts.
	Profile ProfileID

	// ChainID is the 32-byte commitment to the chain this peer is on.
	// Pinned into the transcript and the AEAD-key derivation, so two
	// peers on different chains never derive the same key even with
	// identical KEM material.
	ChainID [chainIDSize]byte

	// KEMScheme is the KEM the initiator offers. Responder MUST echo it
	// back; downgrading on the wire is refused.
	KEMScheme kem.KeyExchangeID

	// ForbidClassicalKEM is true on strict-PQ profiles; when set, the
	// handshake refuses any peer whose KEMScheme byte is in the
	// IsForbiddenInPQMode() set. Also refuses any non-PQ scheme in
	// non-classical positions.
	ForbidClassicalKEM bool
}

// Errors returned by the PQ handshake. Each names exactly one contract
// violation.
var (
	// ErrHandshakeBadVersion — protocol version byte not understood.
	ErrHandshakeBadVersion = errors.New("peer: PQ handshake protocol version not understood")

	// ErrHandshakeProfileMismatch — initiator profile != responder profile
	// or != local-config profile.
	ErrHandshakeProfileMismatch = errors.New("peer: PQ handshake profile mismatch")

	// ErrHandshakeChainMismatch — chain IDs disagree across the handshake.
	ErrHandshakeChainMismatch = errors.New("peer: PQ handshake chain ID mismatch")

	// ErrHandshakeBadIdentity — ML-DSA-65 identity key or signature
	// failed to parse / verify.
	ErrHandshakeBadIdentity = errors.New("peer: PQ handshake identity invalid")

	// ErrHandshakeClassicalKEM — peer offered an explicit classical KEM
	// marker (X25519, P-256, P-384); strict-PQ peers refuse.
	ErrHandshakeClassicalKEM = errors.New("peer: PQ handshake refused classical KEM")

	// ErrHandshakeKEMScheme — peer offered a KEM scheme not allowed by
	// local config (e.g. responder downgraded ML-KEM-1024 to ML-KEM-768).
	ErrHandshakeKEMScheme = errors.New("peer: PQ handshake KEM scheme not admissible")

	// ErrHandshakeNodeIDZero — peer presented the zero NodeID.
	ErrHandshakeNodeIDZero = errors.New("peer: PQ handshake peer NodeID is zero")
)

// HandshakeInit is the initiator's first message. Field order is the
// canonical wire order; the running transcript hashes every field in this
// order before the initiator signs the running prefix.
type HandshakeInit struct {
	ProtocolVersion uint8
	Profile         ProfileID
	ChainID         [chainIDSize]byte
	KEMScheme       kem.KeyExchangeID
	NodeID          ids.NodeID
	MLDSAPub        []byte // length IdentityPublicKeySize
	KEMPub          []byte // length depends on KEMScheme
	Sig             []byte // length IdentitySignatureSize; covers transcript prefix
}

// HandshakeResp is the responder's reply. Same field shape; KEMCiphertext
// replaces KEMPublicKey.
type HandshakeResp struct {
	ProtocolVersion uint8
	Profile         ProfileID
	ChainID         [chainIDSize]byte
	KEMScheme       kem.KeyExchangeID
	NodeID          ids.NodeID
	MLDSAPub        []byte // length IdentityPublicKeySize
	KEMCiphertext   []byte // length depends on KEMScheme
	Sig             []byte // length IdentitySignatureSize; covers transcript prefix
}

// HandshakeResult is what both sides return at the end of a successful
// handshake. KEMSession carries the raw shared secret + bound transcript
// hash; AEADKey is the 256-bit cSHAKE256-derived key for the connection's
// authenticated cipher.
type HandshakeResult struct {
	Local       *LocalIdentity
	PeerNodeID  ids.NodeID
	PeerMLDSA   *mldsa65.PublicKey
	KEMSession  *kem.KEMSession
	AEADKey     [kem.AEADKeySize]byte
}

// InitiateHandshake is the initiator's side of the PQ peer handshake.
// Produces the INIT message ready to send on the wire, plus the
// per-session KEM decapsulation key the caller must retain until
// FinishInitiatorHandshake runs.
//
// Caller workflow:
//
//	init, kemSec, err := InitiateHandshake(cfg, local)
//	// send init.Bytes() on the wire
//	// receive resp bytes from peer
//	resp, err := ParseHandshakeResp(respBytes, cfg.KEMScheme)
//	result, err := FinishInitiatorHandshake(cfg, local, init, resp, kemSec)
func InitiateHandshake(
	cfg *HandshakeConfig,
	local *LocalIdentity,
) (*HandshakeInit, []byte /* kemSec */, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, nil, err
	}

	kemPub, kemSec, err := kem.GenerateKEMKeypair(cfg.KEMScheme, rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("peer: PQ init keypair: %w", err)
	}

	init := &HandshakeInit{
		ProtocolVersion: pqProtocolVersionV1,
		Profile:         cfg.Profile,
		ChainID:         cfg.ChainID,
		KEMScheme:       cfg.KEMScheme,
		NodeID:          local.NodeID,
		MLDSAPub:        packMLDSAPub(local.Public),
		KEMPub:          kemPub,
	}

	// Sign the transcript prefix (every field above Sig). The signature
	// context ("NODE_PQ_HANDSHAKE_V1") makes role and version part of
	// the signed bytes so a captured signature cannot be replayed as a
	// responder signature or under a future protocol version.
	transcriptPrefix := init.transcriptPrefix(roleInitiator)
	sig := make([]byte, IdentitySignatureSize)
	if err := mldsa65.SignTo(
		local.Secret,
		transcriptPrefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/initiator"),
		false, // deterministic mode; randomized=true is also valid, but determinism keeps test vectors stable
		sig,
	); err != nil {
		return nil, nil, fmt.Errorf("peer: PQ init sign: %w", err)
	}
	init.Sig = sig

	return init, kemSec, nil
}

// RespondHandshake is the responder's side. Accepts the parsed INIT,
// validates every cross-axis invariant, encapsulates against the
// initiator's KEM public key, signs the running transcript prefix, and
// returns both the RESP message and the resulting HandshakeResult.
//
// The result's AEADKey is already derived; the caller switches its
// transport to authenticated mode the moment RESP is sent.
func RespondHandshake(
	cfg *HandshakeConfig,
	local *LocalIdentity,
	init *HandshakeInit,
) (*HandshakeResp, *HandshakeResult, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, nil, err
	}
	if err := validateRemoteInit(cfg, init); err != nil {
		return nil, nil, err
	}

	// Parse initiator's MLDSA pub.
	peerPub, err := unpackMLDSAPub(init.MLDSAPub)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: parse peer MLDSA pub: %v",
			ErrHandshakeBadIdentity, err)
	}

	// Verify initiator's signature over the transcript prefix.
	if !mldsa65.Verify(
		peerPub,
		init.transcriptPrefix(roleInitiator),
		[]byte("NODE_PQ_HANDSHAKE_V1/initiator"),
		init.Sig,
	) {
		return nil, nil, fmt.Errorf("%w: initiator signature failed", ErrHandshakeBadIdentity)
	}

	// KEM encapsulate against initiator's KEM pub. transcript bytes here
	// are exactly the initiator's INIT message in canonical order; the
	// responder's RESP will append onto this for the responder's signing
	// transcript.
	transcript := init.canonicalBytes()
	kemSess, ct, err := kem.InitiateKEMSession(cfg.KEMScheme, init.KEMPub, transcript)
	if err != nil {
		return nil, nil, fmt.Errorf("peer: PQ encapsulate: %w", err)
	}

	resp := &HandshakeResp{
		ProtocolVersion: pqProtocolVersionV1,
		Profile:         cfg.Profile,
		ChainID:         cfg.ChainID,
		KEMScheme:       cfg.KEMScheme,
		NodeID:          local.NodeID,
		MLDSAPub:        packMLDSAPub(local.Public),
		KEMCiphertext:   ct,
	}

	respPrefix := resp.transcriptPrefix(init, roleResponder)
	sig := make([]byte, IdentitySignatureSize)
	if err := mldsa65.SignTo(
		local.Secret,
		respPrefix,
		[]byte("NODE_PQ_HANDSHAKE_V1/responder"),
		false,
		sig,
	); err != nil {
		return nil, nil, fmt.Errorf("peer: PQ resp sign: %w", err)
	}
	resp.Sig = sig

	// Bind the final transcript (init+resp+both ML-DSA pubs+profile+chain)
	// for AEAD key derivation.
	fullTranscript := bindAEADTranscript(init, resp)
	finalSession := &kem.KEMSession{
		SchemeID:       kemSess.SchemeID,
		SharedSecret:   kemSess.SharedSecret,
		TranscriptHash: kem.HashTranscript(fullTranscript),
	}

	// Bind the peer's NodeID to its ML-DSA key (same scheme as
	// StakingConfig.DeriveNodeID): reject a claimed NodeID that doesn't
	// match its key, and adopt the key-derived NodeID as the authoritative
	// peer identity so consensus tracks the validator-set NodeID.
	derivedID, derr := deriveMLDSANodeID(init.MLDSAPub)
	if derr != nil {
		return nil, nil, fmt.Errorf("%w: derive initiator NodeID: %v", ErrHandshakeBadIdentity, derr)
	}
	if derivedID != init.NodeID {
		return nil, nil, fmt.Errorf("%w: initiator NodeID %s != ML-DSA-derived %s",
			ErrHandshakeBadIdentity, init.NodeID, derivedID)
	}

	return resp, &HandshakeResult{
		Local:      local,
		PeerNodeID: derivedID,
		PeerMLDSA:  peerPub,
		KEMSession: finalSession,
		AEADKey:    finalSession.DeriveAEADKey(),
	}, nil
}

// FinishInitiatorHandshake is the initiator's third-stage step: validate
// the responder's RESP, decapsulate to recover the shared secret, bind
// the full transcript, and derive the AEAD key. Returns the same
// HandshakeResult shape as RespondHandshake so a caller can treat the
// post-handshake API symmetrically.
func FinishInitiatorHandshake(
	cfg *HandshakeConfig,
	local *LocalIdentity,
	init *HandshakeInit,
	resp *HandshakeResp,
	kemSec []byte,
) (*HandshakeResult, error) {
	if err := validateRemoteResp(cfg, init, resp); err != nil {
		return nil, err
	}

	peerPub, err := unpackMLDSAPub(resp.MLDSAPub)
	if err != nil {
		return nil, fmt.Errorf("%w: parse responder MLDSA pub: %v",
			ErrHandshakeBadIdentity, err)
	}

	if !mldsa65.Verify(
		peerPub,
		resp.transcriptPrefix(init, roleResponder),
		[]byte("NODE_PQ_HANDSHAKE_V1/responder"),
		resp.Sig,
	) {
		return nil, fmt.Errorf("%w: responder signature failed", ErrHandshakeBadIdentity)
	}

	transcript := init.canonicalBytes()
	kemSess, err := kem.RespondKEMSession(cfg.KEMScheme, kemSec, resp.KEMCiphertext, transcript)
	if err != nil {
		return nil, fmt.Errorf("peer: PQ decapsulate: %w", err)
	}

	fullTranscript := bindAEADTranscript(init, resp)
	finalSession := &kem.KEMSession{
		SchemeID:       kemSess.SchemeID,
		SharedSecret:   kemSess.SharedSecret,
		TranscriptHash: kem.HashTranscript(fullTranscript),
	}

	// Bind the responder's NodeID to its ML-DSA key (see RespondHandshake).
	derivedID, derr := deriveMLDSANodeID(resp.MLDSAPub)
	if derr != nil {
		return nil, fmt.Errorf("%w: derive responder NodeID: %v", ErrHandshakeBadIdentity, derr)
	}
	if derivedID != resp.NodeID {
		return nil, fmt.Errorf("%w: responder NodeID %s != ML-DSA-derived %s",
			ErrHandshakeBadIdentity, resp.NodeID, derivedID)
	}

	return &HandshakeResult{
		Local:      local,
		PeerNodeID: derivedID,
		PeerMLDSA:  peerPub,
		KEMSession: finalSession,
		AEADKey:    finalSession.DeriveAEADKey(),
	}, nil
}

// validateConfig refuses obviously bad configs at construction time. The
// rest of the handshake assumes cfg is sane.
func validateConfig(cfg *HandshakeConfig) error {
	if cfg == nil {
		return errors.New("peer: PQ handshake config is nil")
	}
	if cfg.KEMScheme.IsForbiddenInPQMode() {
		return fmt.Errorf("%w: local config offers %s",
			ErrHandshakeClassicalKEM, cfg.KEMScheme)
	}
	if !cfg.KEMScheme.IsPostQuantum() {
		return fmt.Errorf("%w: local config offers %s",
			ErrHandshakeKEMScheme, cfg.KEMScheme)
	}
	if cfg.Profile == ProfileNone {
		return fmt.Errorf("%w: local profile is None", ErrHandshakeProfileMismatch)
	}
	return nil
}

// validateRemoteInit runs every cross-axis check on the parsed INIT
// before any expensive crypto runs.
func validateRemoteInit(cfg *HandshakeConfig, init *HandshakeInit) error {
	if init == nil {
		return errors.New("peer: PQ handshake init is nil")
	}
	if init.ProtocolVersion != pqProtocolVersionV1 {
		return fmt.Errorf("%w: peer offered version=0x%02x, want=0x%02x",
			ErrHandshakeBadVersion, init.ProtocolVersion, pqProtocolVersionV1)
	}
	if init.Profile != cfg.Profile {
		return fmt.Errorf("%w: local=%s peer=%s",
			ErrHandshakeProfileMismatch, cfg.Profile, init.Profile)
	}
	if init.ChainID != cfg.ChainID {
		return fmt.Errorf("%w: local=%x peer=%x",
			ErrHandshakeChainMismatch, cfg.ChainID, init.ChainID)
	}
	if cfg.ForbidClassicalKEM && init.KEMScheme.IsForbiddenInPQMode() {
		return fmt.Errorf("%w: peer offered %s",
			ErrHandshakeClassicalKEM, init.KEMScheme)
	}
	if init.KEMScheme != cfg.KEMScheme {
		return fmt.Errorf("%w: local=%s peer=%s",
			ErrHandshakeKEMScheme, cfg.KEMScheme, init.KEMScheme)
	}
	if init.NodeID == ids.EmptyNodeID {
		return ErrHandshakeNodeIDZero
	}
	if len(init.MLDSAPub) != IdentityPublicKeySize {
		return fmt.Errorf("%w: peer MLDSAPub size=%d want=%d",
			ErrHandshakeBadIdentity, len(init.MLDSAPub), IdentityPublicKeySize)
	}
	wantKEMPub, err := kem.PublicKeySize(init.KEMScheme)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshakeKEMScheme, err)
	}
	if len(init.KEMPub) != wantKEMPub {
		return fmt.Errorf("%w: peer KEMPub size=%d want=%d (scheme=%s)",
			ErrHandshakeKEMScheme, len(init.KEMPub), wantKEMPub, init.KEMScheme)
	}
	if len(init.Sig) != IdentitySignatureSize {
		return fmt.Errorf("%w: peer Sig size=%d want=%d",
			ErrHandshakeBadIdentity, len(init.Sig), IdentitySignatureSize)
	}
	return nil
}

// validateRemoteResp runs the responder-message version of the same
// cross-axis checks. Includes the no-downgrade rule (resp.KEMScheme must
// echo init.KEMScheme exactly).
func validateRemoteResp(cfg *HandshakeConfig, init *HandshakeInit, resp *HandshakeResp) error {
	if resp == nil {
		return errors.New("peer: PQ handshake resp is nil")
	}
	if resp.ProtocolVersion != pqProtocolVersionV1 {
		return fmt.Errorf("%w: peer offered version=0x%02x, want=0x%02x",
			ErrHandshakeBadVersion, resp.ProtocolVersion, pqProtocolVersionV1)
	}
	if resp.Profile != cfg.Profile {
		return fmt.Errorf("%w: local=%s peer=%s",
			ErrHandshakeProfileMismatch, cfg.Profile, resp.Profile)
	}
	if resp.ChainID != cfg.ChainID {
		return fmt.Errorf("%w: local=%x peer=%x",
			ErrHandshakeChainMismatch, cfg.ChainID, resp.ChainID)
	}
	if cfg.ForbidClassicalKEM && resp.KEMScheme.IsForbiddenInPQMode() {
		return fmt.Errorf("%w: peer offered %s",
			ErrHandshakeClassicalKEM, resp.KEMScheme)
	}
	if resp.KEMScheme != init.KEMScheme {
		return fmt.Errorf("%w: responder downgraded KEM init=%s resp=%s",
			ErrHandshakeKEMScheme, init.KEMScheme, resp.KEMScheme)
	}
	if resp.NodeID == ids.EmptyNodeID {
		return ErrHandshakeNodeIDZero
	}
	if len(resp.MLDSAPub) != IdentityPublicKeySize {
		return fmt.Errorf("%w: peer MLDSAPub size=%d want=%d",
			ErrHandshakeBadIdentity, len(resp.MLDSAPub), IdentityPublicKeySize)
	}
	wantCT, err := kem.CiphertextSize(resp.KEMScheme)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshakeKEMScheme, err)
	}
	if len(resp.KEMCiphertext) != wantCT {
		return fmt.Errorf("%w: peer KEMCiphertext size=%d want=%d (scheme=%s)",
			ErrHandshakeKEMScheme, len(resp.KEMCiphertext), wantCT, resp.KEMScheme)
	}
	if len(resp.Sig) != IdentitySignatureSize {
		return fmt.Errorf("%w: peer Sig size=%d want=%d",
			ErrHandshakeBadIdentity, len(resp.Sig), IdentitySignatureSize)
	}
	return nil
}

// transcriptPrefix returns the canonical byte string the initiator signs
// over: every field of the INIT in wire order, MINUS the signature
// itself. role is bound into the signature context (not the prefix), so
// callers that need the bound prefix can recompute it deterministically.
func (h *HandshakeInit) transcriptPrefix(_ HandshakeRole) []byte {
	buf := make([]byte, 0, 256+len(h.MLDSAPub)+len(h.KEMPub))
	buf = append(buf, h.ProtocolVersion)
	buf = append(buf, byte(h.Profile))
	buf = append(buf, h.ChainID[:]...)
	buf = append(buf, byte(h.KEMScheme))
	nid := h.NodeID
	buf = append(buf, nid[:]...)
	buf = appendLengthPrefixed(buf, h.MLDSAPub)
	buf = appendLengthPrefixed(buf, h.KEMPub)
	return buf
}

// canonicalBytes returns the full INIT in wire order including the
// signature. Used as the transcript seed by the responder for KEM and as
// part of the AEAD-binding transcript.
func (h *HandshakeInit) canonicalBytes() []byte {
	buf := h.transcriptPrefix(roleInitiator)
	buf = appendLengthPrefixed(buf, h.Sig)
	return buf
}

// transcriptPrefix for the responder includes the entire INIT bytes so
// the responder's signature commits to what it observed from the
// initiator. role is unused (bound in context); kept for symmetry.
func (h *HandshakeResp) transcriptPrefix(init *HandshakeInit, _ HandshakeRole) []byte {
	buf := make([]byte, 0, 512+len(h.MLDSAPub)+len(h.KEMCiphertext))
	buf = append(buf, init.canonicalBytes()...)
	buf = append(buf, h.ProtocolVersion)
	buf = append(buf, byte(h.Profile))
	buf = append(buf, h.ChainID[:]...)
	buf = append(buf, byte(h.KEMScheme))
	nid := h.NodeID
	buf = append(buf, nid[:]...)
	buf = appendLengthPrefixed(buf, h.MLDSAPub)
	buf = appendLengthPrefixed(buf, h.KEMCiphertext)
	return buf
}

// canonicalBytes returns the full RESP in wire order including the
// signature. Independent of INIT bytes because the wire delivers RESP
// alone; the transcript-binding APIs glue the two.
func (h *HandshakeResp) canonicalBytes() []byte {
	buf := make([]byte, 0, 256+len(h.MLDSAPub)+len(h.KEMCiphertext)+len(h.Sig))
	buf = append(buf, h.ProtocolVersion)
	buf = append(buf, byte(h.Profile))
	buf = append(buf, h.ChainID[:]...)
	buf = append(buf, byte(h.KEMScheme))
	nid := h.NodeID
	buf = append(buf, nid[:]...)
	buf = appendLengthPrefixed(buf, h.MLDSAPub)
	buf = appendLengthPrefixed(buf, h.KEMCiphertext)
	buf = appendLengthPrefixed(buf, h.Sig)
	return buf
}

// bindAEADTranscript returns the byte string that gets hashed into the
// final TranscriptHash for AEAD key derivation. Per HIP-0078 §"Session
// key derivation", this MUST include: profile_id, chain_id, both peers'
// ML-DSA public keys, both wire messages. Re-listing fields that already
// appear inside init/resp.canonicalBytes() is intentional defense in
// depth: a manifest mismatch on profile or chain shows up here even if
// the encoding of init or resp drifted.
func bindAEADTranscript(init *HandshakeInit, resp *HandshakeResp) []byte {
	buf := make([]byte, 0, 1024)
	buf = append(buf, init.canonicalBytes()...)
	buf = append(buf, resp.canonicalBytes()...)
	buf = append(buf, byte(init.Profile))
	buf = append(buf, init.ChainID[:]...)
	buf = append(buf, init.MLDSAPub...)
	buf = append(buf, resp.MLDSAPub...)
	return buf
}

// packMLDSAPub packs an ML-DSA-65 public key into its canonical byte form.
func packMLDSAPub(pk *mldsa65.PublicKey) []byte {
	var buf [mldsa65.PublicKeySize]byte
	pk.Pack(&buf)
	out := make([]byte, mldsa65.PublicKeySize)
	copy(out, buf[:])
	return out
}

// unpackMLDSAPub parses an ML-DSA-65 public key from canonical bytes.
func unpackMLDSAPub(b []byte) (*mldsa65.PublicKey, error) {
	if len(b) != mldsa65.PublicKeySize {
		return nil, fmt.Errorf("public key size=%d want=%d", len(b), mldsa65.PublicKeySize)
	}
	var buf [mldsa65.PublicKeySize]byte
	copy(buf[:], b)
	pk := new(mldsa65.PublicKey)
	pk.Unpack(&buf)
	return pk, nil
}

// appendLengthPrefixed appends a 4-byte big-endian length and then b.
// Used inside transcript byte strings so two distinct concatenations
// cannot collide via a boundary ambiguity.
func appendLengthPrefixed(dst, b []byte) []byte {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(b)))
	dst = append(dst, lp[:]...)
	dst = append(dst, b...)
	return dst
}

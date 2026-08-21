// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	stdbytes "bytes"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/zap"
)

var (
	_ SignedBlock = (*statelessBlock)(nil)

	errUnexpectedSignature     = errors.New("signature provided when none was expected")
	errInvalidCertificate      = errors.New("invalid certificate")
	errUnknownProposerScheme   = errors.New("proposervm block: unknown proposer identity scheme")
	errMLDSAProposerSigInvalid = errors.New("proposervm block: ML-DSA proposer signature invalid")
	errNonCanonicalWire        = errors.New("proposervm block: non-canonical wire encoding")
)

// Proposer-identity scheme tags stored as the FIRST byte of the offCert slot,
// so a block is self-describing about which primitive proves its proposer.
// They are the SAME bytes as the canonical wire NodeID scheme (ids.NodeIDScheme):
// 0x90 classical secp256k1/ECDSA TLS leaf, 0x42 strict-PQ ML-DSA-65. Keeping the
// tag in the block means a verifier dispatches to the correct verifier + the
// correct NodeID derivation WITHOUT consulting the chain profile — the block
// carries its own identity discriminator, exactly like a TypedNodeID does on the
// handshake. The proposer NodeID a signed block reports MUST equal the NodeID the
// windower elected; the windower reads the P-chain validator set, whose NodeIDs
// are ids.NodeIDFromCert (classical) or DeriveMLDSA(ids.Empty, pub) (strict-PQ) —
// so the two branches below reproduce exactly those two derivations.
const (
	schemeSecp256k1 = byte(ids.NodeIDSchemeSecp256k1) // 0x90
	schemeMLDSA65   = byte(ids.NodeIDSchemeMLDSA65)   // 0x42
)

// Epoch represents a P-Chain epoch for validator set coordination
type Epoch struct {
	PChainHeight uint64 `json:"pChainHeight"`
	Number       uint64 `json:"number"`
	StartTime    int64  `json:"startTime"`
}

type Block interface {
	ID() ids.ID
	ParentID() ids.ID
	Block() []byte
	Bytes() []byte

	initialize(bytes []byte) error
	verify(chainID ids.ID) error
}

type SignedBlock interface {
	Block

	PChainHeight() uint64
	PChainEpoch() Epoch
	Timestamp() time.Time

	// Proposer returns the ID of the node that proposed this block. If no node
	// signed this block, [ids.EmptyNodeID] will be returned.
	Proposer() ids.NodeID

	// HasClassicalProposer reports whether this block carries a CLASSICAL
	// (secp256k1/ECDSA) proposer identity. A strict-PQ chain refuses such a block
	// — its proposer must be ML-DSA-65 to match the ML-DSA-keyed validator set.
	// Unsigned blocks (transition / single-validator) return false.
	HasClassicalProposer() bool

	// Data Availability fields (v1.1 spec)
	DARoot() [32]byte          // Root of DA commitments
	WitnessRoot() [32]byte     // Root of witnesses/proofs
	MessagesOutRoot() [32]byte // Root of outgoing cross-chain messages
	BlobCount() uint32         // Number of DA blobs in block
}

// statelessBlock is zap-backed: msg is the unsigned body buffer (a
// self-delimiting zap message with blkSigned at offset 0), Signature is the
// optional proposer signature carried in the appended suffix buffer. All block
// fields are read from msg via fixed offsets — the struct IS the wire.
type statelessBlock struct {
	msg       *zap.Message // unsigned body buffer
	Signature []byte       // proposer signature (appended suffix); exported for tests

	id        ids.ID
	timestamp time.Time
	scheme    byte                 // proposer-identity scheme (0 for unsigned)
	cert      *staking.Certificate // set for the classical (secp256k1) scheme
	mldsaPub  *mldsa.PublicKey     // set for the strict-PQ (ML-DSA-65) scheme
	proposer  ids.NodeID
	bytes     []byte // full signed bytes = unsigned ‖ sig
}

func (b *statelessBlock) ID() ids.ID {
	return b.id
}

func (b *statelessBlock) ParentID() ids.ID {
	return ids.ID(read32(b.msg.Root(), offParentID))
}

func (b *statelessBlock) Block() []byte {
	return b.msg.Root().Bytes(offBlock)
}

func (b *statelessBlock) Bytes() []byte {
	return b.bytes
}

// initialize binds the block's fields from its signed bytes. This is the one
// bytes→fields entry, shared by the Build* constructors (fresh buffer) and
// Parse (wire/disk buffer); the bytes are authoritative and never re-encoded.
func (b *statelessBlock) initialize(bytes []byte) error {
	b.bytes = bytes

	// The signed form is unsigned_buffer ‖ sig_buffer (both self-delimiting).
	// Split on the leading message length; the ID is the hash of that prefix.
	n, err := zapLen(bytes)
	if err != nil {
		return err
	}
	msg, err := zap.Parse(bytes[:n])
	if err != nil {
		return err
	}
	b.msg = msg
	b.id = hash.ComputeHash256Array(bytes[:n])

	if len(bytes) > n {
		sigBytes := bytes[n:]
		sigMsg, err := zap.Parse(sigBytes)
		if err != nil {
			return err
		}
		if s := sigMsg.Root().Bytes(offSig); len(s) > 0 {
			b.Signature = append([]byte(nil), s...)
		}
		// The signature suffix is outside the block ID, so accepting multiple
		// encodings of it would let a peer persist and re-serve arbitrary bytes
		// under somebody else's valid block ID. Rebuild the only canonical form
		// and require byte-for-byte equality; this also rejects trailing data.
		if !stdbytes.Equal(sigBytes, buildSigBuffer(b.Signature)) {
			return errNonCanonicalWire
		}
	}

	root := b.msg.Root()
	b.timestamp = decodeTimestamp(root.Int64(offTimestamp))

	// Proposer-identity slot: [scheme:1B | identity]. Empty ⇒ unsigned block.
	idSlot := root.Bytes(offCert)
	if len(idSlot) == 0 {
		if len(bytes) != n {
			return errNonCanonicalWire
		}
		return nil
	}
	b.scheme = idSlot[0]
	identity := idSlot[1:]
	switch b.scheme {
	case schemeMLDSA65:
		// Strict-PQ: identity is the raw ML-DSA-65 public key. Derive the proposer
		// NodeID the SAME way the node derives its own canonical NodeID and the
		// windower reads it — DeriveMLDSA over the empty chain id (node.go boots with
		// DeriveNodeID(ids.Empty)). This is what makes Proposer() == the windower's
		// elected NodeID under strict-PQ.
		pub, err := mldsa.PublicKeyFromBytes(identity, mldsa.MLDSA65)
		if err != nil {
			return fmt.Errorf("%w: %w", errInvalidCertificate, err)
		}
		b.mldsaPub = pub
		nodeID, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(ids.Empty, identity)
		if err != nil {
			return err
		}
		b.proposer = nodeID
	case schemeSecp256k1:
		// Classical: identity is the DER TLS cert. NodeID = hash of the cert, the
		// legacy upstream derivation used on non-strict-PQ chains.
		b.cert, err = staking.ParseCertificate(identity)
		if err != nil {
			return fmt.Errorf("%w: %w", errInvalidCertificate, err)
		}
		b.proposer = ids.NodeIDFromCert(&ids.Certificate{
			Raw:       b.cert.Raw,
			PublicKey: b.cert.PublicKey,
		})
	default:
		return fmt.Errorf("%w: 0x%02x", errUnknownProposerScheme, b.scheme)
	}
	return nil
}

func (b *statelessBlock) verify(chainID ids.ID) error {
	if b.cert == nil && b.mldsaPub == nil {
		if len(b.Signature) > 0 {
			return errUnexpectedSignature
		}
		return nil
	}

	header, err := BuildHeader(chainID, b.ParentID(), b.id)
	if err != nil {
		return err
	}
	headerBytes := header.Bytes()

	if b.mldsaPub != nil {
		// FIPS 204 §5.2 domain-separated verification: the same proposervm context
		// the signer bound, so a proposer signature can never be replayed as any
		// other ML-DSA message (UTXO auth, consensus vote) the validator produces.
		if !b.mldsaPub.VerifySignatureCtx(headerBytes, b.Signature, proposerSigCtx) {
			return errMLDSAProposerSigInvalid
		}
		return nil
	}

	return staking.CheckSignature(
		b.cert,
		headerBytes,
		b.Signature,
	)
}

func (b *statelessBlock) PChainHeight() uint64 {
	return b.msg.Root().Uint64(offPChainHt)
}

func (b *statelessBlock) PChainEpoch() Epoch {
	root := b.msg.Root()
	return Epoch{
		PChainHeight: root.Uint64(offEpochHt),
		Number:       root.Uint64(offEpochNum),
		StartTime:    root.Int64(offEpochStart),
	}
}

func (b *statelessBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *statelessBlock) Proposer() ids.NodeID {
	return b.proposer
}

func (b *statelessBlock) HasClassicalProposer() bool {
	return b.scheme == schemeSecp256k1
}

func (b *statelessBlock) DARoot() [32]byte {
	return read32(b.msg.Root(), offDARoot)
}

func (b *statelessBlock) WitnessRoot() [32]byte {
	return read32(b.msg.Root(), offWitnessRoot)
}

func (b *statelessBlock) MessagesOutRoot() [32]byte {
	return read32(b.msg.Root(), offMsgOutRoot)
}

func (b *statelessBlock) BlobCount() uint32 {
	return b.msg.Root().Uint32(offBlobCount)
}

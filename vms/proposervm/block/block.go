// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/zap"
)

var (
	_ SignedBlock = (*statelessBlock)(nil)

	errUnexpectedSignature = errors.New("signature provided when none was expected")
	errInvalidCertificate  = errors.New("invalid certificate")
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
	cert      *staking.Certificate
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
		sigMsg, err := zap.Parse(bytes[n:])
		if err != nil {
			return err
		}
		if s := sigMsg.Root().Bytes(offSig); len(s) > 0 {
			b.Signature = append([]byte(nil), s...)
		}
	}

	root := b.msg.Root()
	b.timestamp = time.Unix(root.Int64(offTimestamp), 0)

	certBytes := root.Bytes(offCert)
	if len(certBytes) == 0 {
		return nil
	}

	b.cert, err = staking.ParseCertificate(certBytes)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidCertificate, err)
	}

	b.proposer = ids.NodeIDFromCert(&ids.Certificate{
		Raw:       b.cert.Raw,
		PublicKey: b.cert.PublicKey,
	})
	return nil
}

func (b *statelessBlock) verify(chainID ids.ID) error {
	if b.cert == nil {
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

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
)

// Block represents a block in the Beam consensus
type Block struct {
	PrntID     ids.ID
	Hght       uint64
	Tmstmp     int64
	ProposerID ids.NodeID
	TxList     [][]byte
	Certs      CertBundle

	id     ids.ID
	bytes  []byte
	status choices.Status
}

// CertBundle contains dual certificates for quantum security
type CertBundle struct {
	BLSAgg [96]byte // BLS aggregate signature
	RTCert []byte   // Ringtail certificate (~3KB)
}

// ID returns the ID of this block
func (b *Block) ID() ids.ID {
	if b.id == ids.Empty {
		b.id = b.calculateID()
	}
	return b.id
}

// Parent returns the parent block ID
func (b *Block) Parent() ids.ID {
	return b.PrntID
}

// Height returns the height of this block
func (b *Block) Height() uint64 {
	return b.Hght
}

// Timestamp returns the timestamp of this block
func (b *Block) Timestamp() int64 {
	return b.Tmstmp
}

// Bytes returns the byte representation of this block
func (b *Block) Bytes() []byte {
	if b.bytes == nil {
		b.bytes = b.calculateBytes()
	}
	return b.bytes
}

// Verify verifies the block
func (b *Block) Verify() error {
	// Verify timestamp
	if b.Tmstmp > time.Now().Unix() {
		return errors.New("block timestamp is in the future")
	}

	// Verify height
	if b.Hght == 0 && b.PrntID != ids.Empty {
		return errors.New("genesis block must have empty parent")
	}

	// Verify dual certificates if present
	if !b.isGenesis() {
		if err := b.verifyDualCertificates(); err != nil {
			return err
		}
	}

	return nil
}

// Accept marks this block as accepted
func (b *Block) Accept() error {
	if b.status == choices.Rejected {
		return errors.New("cannot accept rejected block")
	}
	b.status = choices.Accepted
	return nil
}

// Reject marks this block as rejected
func (b *Block) Reject() error {
	if b.status == choices.Accepted || b.status == choices.Quantum {
		return errors.New("cannot reject accepted block")
	}
	b.status = choices.Rejected
	return nil
}

// Status returns the current status
func (b *Block) Status() choices.Status {
	return b.status
}

// SetStatus sets the status
func (b *Block) SetStatus(status choices.Status) {
	b.status = status
}

// HasDualCert returns true if both BLS and RT certificates are present
func (b *Block) HasDualCert() bool {
	return b.hasBLSCert() && b.hasRTCert()
}

// BLSSignature returns the BLS aggregate signature
func (b *Block) BLSSignature() []byte {
	return b.Certs.BLSAgg[:]
}

// RTCertificate returns the Ringtail certificate
func (b *Block) RTCertificate() []byte {
	return b.Certs.RTCert
}

// SetQuantum marks this block as having quantum-secure finality
func (b *Block) SetQuantum() error {
	if !b.HasDualCert() {
		return errors.New("cannot set quantum status without dual certificates")
	}
	if b.status != choices.Accepted {
		return errors.New("can only set quantum status on accepted blocks")
	}
	b.status = choices.Quantum
	return nil
}

// calculateID calculates the block ID
func (b *Block) calculateID() ids.ID {
	bytes := b.Bytes()
	hash := sha256.Sum256(bytes)
	return ids.ID(hash)
}

// calculateBytes calculates the byte representation
func (b *Block) calculateBytes() []byte {
	buf := new(bytes.Buffer)

	// Write parent ID
	buf.Write(b.PrntID[:])

	// Write height
	binary.Write(buf, binary.LittleEndian, b.Hght)

	// Write timestamp
	binary.Write(buf, binary.LittleEndian, b.Tmstmp)

	// Write proposer ID
	buf.Write(b.ProposerID[:])

	// Write number of transactions
	binary.Write(buf, binary.LittleEndian, uint32(len(b.TxList)))

	// Write transactions
	for _, tx := range b.TxList {
		binary.Write(buf, binary.LittleEndian, uint32(len(tx)))
		buf.Write(tx)
	}

	// Write certificates
	buf.Write(b.Certs.BLSAgg[:])
	binary.Write(buf, binary.LittleEndian, uint32(len(b.Certs.RTCert)))
	buf.Write(b.Certs.RTCert)

	return buf.Bytes()
}

// isGenesis returns true if this is the genesis block
func (b *Block) isGenesis() bool {
	return b.Hght == 0
}

// hasBLSCert returns true if BLS certificate is present
func (b *Block) hasBLSCert() bool {
	return b.Certs.BLSAgg != [96]byte{}
}

// hasRTCert returns true if RT certificate is present
func (b *Block) hasRTCert() bool {
	return len(b.Certs.RTCert) > 0
}

// verifyDualCertificates verifies both BLS and RT certificates
func (b *Block) verifyDualCertificates() error {
	// Verify BLS certificate
	if !b.hasBLSCert() {
		return errors.New("missing BLS certificate")
	}

	// Verify RT certificate
	if !b.hasRTCert() {
		return errors.New("missing Ringtail certificate")
	}

	// In production, these would call actual verification functions
	// For now, we just check they exist

	return nil
}

// BuildBlock creates a new block
func BuildBlock(
	parentID ids.ID,
	height uint64,
	timestamp int64,
	proposerID ids.NodeID,
	txs [][]byte,
) *Block {
	return &Block{
		PrntID:     parentID,
		Hght:       height,
		Tmstmp:     timestamp,
		ProposerID: proposerID,
		TxList:     txs,
		status:     choices.Processing,
	}
}

// AttachCertificates attaches dual certificates to a block
func (b *Block) AttachCertificates(blsAgg [96]byte, rtCert []byte) error {
	if b.hasBLSCert() || b.hasRTCert() {
		return errors.New("certificates already attached")
	}

	b.Certs.BLSAgg = blsAgg
	b.Certs.RTCert = rtCert

	// Recalculate ID and bytes with certificates
	b.id = ids.Empty
	b.bytes = nil

	return nil
}

// ParseBlock parses a block from bytes
func ParseBlock(data []byte) (*Block, error) {
	buf := bytes.NewReader(data)
	b := &Block{}

	// Read parent ID
	if _, err := buf.Read(b.PrntID[:]); err != nil {
		return nil, err
	}

	// Read height
	if err := binary.Read(buf, binary.LittleEndian, &b.Hght); err != nil {
		return nil, err
	}

	// Read timestamp
	if err := binary.Read(buf, binary.LittleEndian, &b.Tmstmp); err != nil {
		return nil, err
	}

	// Read proposer ID
	if _, err := buf.Read(b.ProposerID[:]); err != nil {
		return nil, err
	}

	// Read number of transactions
	var numTxs uint32
	if err := binary.Read(buf, binary.LittleEndian, &numTxs); err != nil {
		return nil, err
	}

	// Read transactions
	b.TxList = make([][]byte, numTxs)
	for i := uint32(0); i < numTxs; i++ {
		var txLen uint32
		if err := binary.Read(buf, binary.LittleEndian, &txLen); err != nil {
			return nil, err
		}
		tx := make([]byte, txLen)
		if _, err := buf.Read(tx); err != nil {
			return nil, err
		}
		b.TxList[i] = tx
	}

	// Read BLS aggregate
	if _, err := buf.Read(b.Certs.BLSAgg[:]); err != nil {
		return nil, err
	}

	// Read RT cert length
	var rtLen uint32
	if err := binary.Read(buf, binary.LittleEndian, &rtLen); err != nil {
		return nil, err
	}

	// Read RT cert
	if rtLen > 0 {
		b.Certs.RTCert = make([]byte, rtLen)
		if _, err := buf.Read(b.Certs.RTCert); err != nil {
			return nil, err
		}
	}

	b.status = choices.Processing
	b.bytes = data

	return b, nil
}
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/luxfi/node/network/kem"
)

// pqFrameMaxSize is the upper bound on a single PQ-handshake frame (INIT
// or RESP). Both messages carry an ML-DSA-65 signature (3309 B), an
// ML-DSA-65 public key (1952 B), an ML-KEM-1024 public key (1568 B) or
// ciphertext (1568 B), plus version / profile / chain-id / NodeID fields
// (~70 B). 16 KiB is comfortably above the largest legitimate frame and
// well below any DoS-relevant threshold.
const pqFrameMaxSize = 16 * 1024

// errPQFrameTooLarge is returned when a peer announces a PQ-handshake
// frame larger than pqFrameMaxSize. Refusing oversize frames before the
// allocation prevents a memory-exhaustion DoS.
var errPQFrameTooLarge = errors.New("peer: PQ handshake frame exceeds size cap")

// readPQFrame reads a single 4-byte big-endian length-prefixed PQ frame
// from conn. Mirrors the framing scheme the rest of the peer wire uses;
// strict-PQ chains carry one INIT + one RESP via these calls before any
// p2p message flows.
func readPQFrame(conn net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, fmt.Errorf("read PQ frame header: %w", err)
	}
	size := binary.BigEndian.Uint32(hdr[:])
	if size > pqFrameMaxSize {
		return nil, fmt.Errorf("%w: %d > %d", errPQFrameTooLarge, size, pqFrameMaxSize)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("read PQ frame body: %w", err)
	}
	return buf, nil
}

// writePQFrame writes a single 4-byte big-endian length-prefixed PQ
// frame onto conn. Symmetric with readPQFrame.
func writePQFrame(conn net.Conn, payload []byte) error {
	if len(payload) > pqFrameMaxSize {
		return fmt.Errorf("%w: payload=%d > cap=%d",
			errPQFrameTooLarge, len(payload), pqFrameMaxSize)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return fmt.Errorf("write PQ frame header: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("write PQ frame body: %w", err)
	}
	return nil
}

// parsePQHandshakeInit decodes the canonical wire form of HandshakeInit.
// Field layout matches HandshakeInit.canonicalBytes / transcriptPrefix in
// handshake.go. The KEM scheme parameter pins the expected lengths of
// KEMPub and Sig — passing the wrong scheme produces a length mismatch
// error before any signature work runs.
func parsePQHandshakeInit(b []byte, scheme kem.KeyExchangeID) (*HandshakeInit, error) {
	r := newPQReader(b)
	h := &HandshakeInit{}
	var err error
	if h.ProtocolVersion, err = r.readU8(); err != nil {
		return nil, err
	}
	profByte, err := r.readU8()
	if err != nil {
		return nil, err
	}
	h.Profile = ProfileID(profByte)
	if err := r.readFixed(h.ChainID[:]); err != nil {
		return nil, err
	}
	kemByte, err := r.readU8()
	if err != nil {
		return nil, err
	}
	h.KEMScheme = kem.KeyExchangeID(kemByte)
	if h.KEMScheme != scheme {
		return nil, fmt.Errorf("%w: peer offered %s, expected %s",
			ErrHandshakeKEMScheme, h.KEMScheme, scheme)
	}
	var nid [nodeIDSize]byte
	if err := r.readFixed(nid[:]); err != nil {
		return nil, err
	}
	copy(h.NodeID[:], nid[:])
	if h.MLDSAPub, err = r.readBytes(); err != nil {
		return nil, err
	}
	if h.KEMPub, err = r.readBytes(); err != nil {
		return nil, err
	}
	if h.Sig, err = r.readBytes(); err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("%w: trailing bytes in PQ INIT (%d)",
			ErrHandshakeBadIdentity, r.remaining())
	}
	return h, nil
}

// parsePQHandshakeResp decodes the canonical wire form of HandshakeResp.
// Symmetric with parsePQHandshakeInit; KEMCiphertext replaces KEMPub.
func parsePQHandshakeResp(b []byte, scheme kem.KeyExchangeID) (*HandshakeResp, error) {
	r := newPQReader(b)
	h := &HandshakeResp{}
	var err error
	if h.ProtocolVersion, err = r.readU8(); err != nil {
		return nil, err
	}
	profByte, err := r.readU8()
	if err != nil {
		return nil, err
	}
	h.Profile = ProfileID(profByte)
	if err := r.readFixed(h.ChainID[:]); err != nil {
		return nil, err
	}
	kemByte, err := r.readU8()
	if err != nil {
		return nil, err
	}
	h.KEMScheme = kem.KeyExchangeID(kemByte)
	if h.KEMScheme != scheme {
		return nil, fmt.Errorf("%w: peer offered %s, expected %s",
			ErrHandshakeKEMScheme, h.KEMScheme, scheme)
	}
	var nid [nodeIDSize]byte
	if err := r.readFixed(nid[:]); err != nil {
		return nil, err
	}
	copy(h.NodeID[:], nid[:])
	if h.MLDSAPub, err = r.readBytes(); err != nil {
		return nil, err
	}
	if h.KEMCiphertext, err = r.readBytes(); err != nil {
		return nil, err
	}
	if h.Sig, err = r.readBytes(); err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("%w: trailing bytes in PQ RESP (%d)",
			ErrHandshakeBadIdentity, r.remaining())
	}
	return h, nil
}

// pqReader is a tiny zero-allocation reader for the PQ handshake wire
// format. Kept local to this file so the encoding stays reviewable in
// one place; the writer side lives on the HandshakeInit / HandshakeResp
// methods in handshake.go.
type pqReader struct {
	buf    []byte
	offset int
}

func newPQReader(b []byte) *pqReader { return &pqReader{buf: b} }

func (r *pqReader) remaining() int { return len(r.buf) - r.offset }

func (r *pqReader) readU8() (uint8, error) {
	if r.remaining() < 1 {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.buf[r.offset]
	r.offset++
	return v, nil
}

func (r *pqReader) readFixed(dst []byte) error {
	if r.remaining() < len(dst) {
		return io.ErrUnexpectedEOF
	}
	copy(dst, r.buf[r.offset:])
	r.offset += len(dst)
	return nil
}

func (r *pqReader) readBytes() ([]byte, error) {
	if r.remaining() < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	n := binary.BigEndian.Uint32(r.buf[r.offset:])
	r.offset += 4
	if uint64(n) > uint64(r.remaining()) {
		return nil, io.ErrUnexpectedEOF
	}
	out := make([]byte, n)
	copy(out, r.buf[r.offset:r.offset+int(n)])
	r.offset += int(n)
	return out, nil
}

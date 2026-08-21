// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/kem"
)

// scriptConn is a net.Conn whose reads come from a fixed script of bytes and
// whose writes are captured. Nothing here talks to a socket: the PQ frame
// reader is a pure function of the bytes a peer sends, so the bytes are the
// whole test surface.
type scriptConn struct {
	r         bytes.Reader
	w         bytes.Buffer
	writeErr  error
	shortWrit bool
}

func newScriptConn(script []byte) *scriptConn {
	c := &scriptConn{}
	c.r.Reset(script)
	return c
}

func (c *scriptConn) Read(b []byte) (int, error) { return c.r.Read(b) }

func (c *scriptConn) Write(b []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.shortWrit {
		// Report fewer bytes than accepted without an error — the pathological
		// io.Writer contract violation a hostile transport could exhibit.
		return c.w.Write(b[:0])
	}
	return c.w.Write(b)
}

func (*scriptConn) Close() error                       { return nil }
func (*scriptConn) LocalAddr() net.Addr                { return nil }
func (*scriptConn) RemoteAddr() net.Addr               { return nil }
func (*scriptConn) SetDeadline(time.Time) error        { return nil }
func (*scriptConn) SetReadDeadline(time.Time) error    { return nil }
func (*scriptConn) SetWriteDeadline(time.Time) error   { return nil }

// header returns the 4-byte big-endian length prefix for n.
func header(n uint32) []byte {
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], n)
	return h[:]
}

// TestPQFrame_RoundTrip pins the framing invariant both sides depend on:
// what writePQFrame puts on the wire, readPQFrame hands back byte for byte,
// at every boundary length including empty and the cap itself.
func TestPQFrame_RoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 4, 255, 256, 4096, pqFrameMaxSize - 1, pqFrameMaxSize} {
		payload := bytes.Repeat([]byte{0xA7}, size)
		for i := range payload {
			payload[i] = byte(i)
		}

		w := newScriptConn(nil)
		require.NoError(t, writePQFrame(w, payload), "size=%d", size)

		r := newScriptConn(w.w.Bytes())
		got, err := readPQFrame(r)
		require.NoError(t, err, "size=%d", size)
		require.Equal(t, payload, got, "size=%d: frame must survive the wire unchanged", size)
		require.Equal(t, 0, r.r.Len(), "size=%d: reader must consume exactly one frame", size)
	}
}

// TestPQFrame_AnnouncedLengthCannotSizeAnAllocation is the DoS property. A
// peer controls the 4-byte prefix outright; if the reader believed it, one
// 4-byte send would ask for 4 GiB. The cap must be enforced from the header
// alone, before the body read and before any allocation, and the frame that
// gets refused must be the oversize one — not the connection's next frame.
func TestPQFrame_AnnouncedLengthCannotSizeAnAllocation(t *testing.T) {
	for _, announced := range []uint32{
		pqFrameMaxSize + 1,
		1 << 20,
		1 << 30,
		1<<31 - 1,
		1 << 31,
		^uint32(0), // 4 GiB - 1
	} {
		// The body is NOT supplied: a reader that allocated first and read
		// second would either hang or blow up here. Only a reader that
		// checks the header refuses cheaply.
		conn := newScriptConn(header(announced))

		got, err := readPQFrame(conn)
		require.Nil(t, got, "announced=%d", announced)
		require.ErrorIs(t, err, errPQFrameTooLarge,
			"announced=%d: a peer-supplied length above the cap must be refused on the header", announced)
	}
}

// TestPQFrame_CapBoundaryIsExact fixes the cap on the value, not near it: one
// byte under is a legal frame, the cap itself is legal, one byte over is
// refused. An off-by-one either way is a silently different protocol.
func TestPQFrame_CapBoundaryIsExact(t *testing.T) {
	atCap := bytes.Repeat([]byte{0x11}, pqFrameMaxSize)
	w := newScriptConn(nil)
	require.NoError(t, writePQFrame(w, atCap))

	got, err := readPQFrame(newScriptConn(w.w.Bytes()))
	require.NoError(t, err, "a frame of exactly pqFrameMaxSize is legal")
	require.Len(t, got, pqFrameMaxSize)

	overCap := bytes.Repeat([]byte{0x11}, pqFrameMaxSize+1)
	require.ErrorIs(t, writePQFrame(newScriptConn(nil), overCap), errPQFrameTooLarge,
		"the writer must refuse to emit a frame its own reader would reject")

	// And the reader refuses the same length arriving from a peer.
	overWire := append(header(pqFrameMaxSize+1), overCap...)
	_, err = readPQFrame(newScriptConn(overWire))
	require.ErrorIs(t, err, errPQFrameTooLarge)
}

// TestPQFrame_TruncationNeverYieldsAFrame walks every prefix of a real frame.
// No prefix short of the whole thing may produce a payload: a handshake built
// from a half-delivered frame is a handshake over attacker-chosen padding.
func TestPQFrame_TruncationNeverYieldsAFrame(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5A}, 300)
	w := newScriptConn(nil)
	require.NoError(t, writePQFrame(w, payload))
	whole := w.w.Bytes()

	for cut := 0; cut < len(whole); cut++ {
		got, err := readPQFrame(newScriptConn(whole[:cut]))
		require.Nil(t, got, "prefix of %d bytes must not decode", cut)
		require.Error(t, err, "prefix of %d bytes must not decode", cut)
		require.True(t,
			errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF),
			"prefix of %d bytes: want a truncation error, got %v", cut, err)
	}

	got, err := readPQFrame(newScriptConn(whole))
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

// TestPQFrame_WriteErrorsAreReported keeps a failed send from looking like a
// sent frame: a transport error on either the header or the body write must
// surface, because the caller goes on to wait for a reply that will never come.
func TestPQFrame_WriteErrorsAreReported(t *testing.T) {
	boom := errors.New("transport is gone")

	c := newScriptConn(nil)
	c.writeErr = boom
	require.ErrorIs(t, writePQFrame(c, []byte{1, 2, 3}), boom)

	// Header succeeds, body fails: still an error, never a silent partial frame.
	c2 := &halfWriteConn{failAfter: 1, err: boom}
	require.ErrorIs(t, writePQFrame(c2, []byte{1, 2, 3}), boom)
}

// halfWriteConn accepts failAfter successful Writes and then fails, modelling
// a connection that dies between the frame header and its body.
type halfWriteConn struct {
	scriptConn
	failAfter int
	n         int
	err       error
}

func (c *halfWriteConn) Write(b []byte) (int, error) {
	if c.n >= c.failAfter {
		return 0, c.err
	}
	c.n++
	return len(b), nil
}

// ---------------------------------------------------------------------------
// INIT / RESP body parsing
// ---------------------------------------------------------------------------

// pqTestConfig is a strict-PQ handshake config on the supplied scheme.
func pqTestConfig(chainID [chainIDSize]byte, scheme kem.KeyExchangeID) *HandshakeConfig {
	return &HandshakeConfig{
		Profile:            ProfileStrictPQ,
		ChainID:            chainID,
		KEMScheme:          scheme,
		ForbidClassicalKEM: true,
	}
}

// newWireMessages produces a genuine INIT and RESP pair plus their canonical
// wire bytes, for tests that need real crypto to mangle.
func newWireMessages(t *testing.T, scheme kem.KeyExchangeID) (*HandshakeInit, *HandshakeResp, *HandshakeConfig) {
	t.Helper()
	initiator, responder, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, scheme)

	init, _, err := InitiateHandshake(cfg, initiator)
	require.NoError(t, err)
	resp, _, err := RespondHandshake(cfg, responder, init)
	require.NoError(t, err)
	return init, resp, cfg
}

// TestPQBody_RoundTripsCanonicalBytes is the parse/serialize property: every
// field a peer sent comes back identical, so nothing on the wire is dropped,
// reordered or widened on the way in.
func TestPQBody_RoundTripsCanonicalBytes(t *testing.T) {
	for _, scheme := range []kem.KeyExchangeID{kem.KeyExchangeMLKEM768, kem.KeyExchangeMLKEM1024} {
		init, resp, _ := newWireMessages(t, scheme)

		gotInit, err := parsePQHandshakeInit(init.canonicalBytes(), scheme)
		require.NoError(t, err)
		require.Equal(t, init.ProtocolVersion, gotInit.ProtocolVersion)
		require.Equal(t, init.Profile, gotInit.Profile)
		require.Equal(t, init.ChainID, gotInit.ChainID)
		require.Equal(t, init.KEMScheme, gotInit.KEMScheme)
		require.Equal(t, init.NodeID, gotInit.NodeID)
		require.Equal(t, init.MLDSAPub, gotInit.MLDSAPub)
		require.Equal(t, init.KEMPub, gotInit.KEMPub)
		require.Equal(t, init.Sig, gotInit.Sig)
		require.Equal(t, init.canonicalBytes(), gotInit.canonicalBytes(),
			"re-serializing what we parsed must reproduce the wire bytes exactly")

		gotResp, err := parsePQHandshakeResp(resp.canonicalBytes(), scheme)
		require.NoError(t, err)
		require.Equal(t, resp.NodeID, gotResp.NodeID)
		require.Equal(t, resp.KEMCiphertext, gotResp.KEMCiphertext)
		require.Equal(t, resp.Sig, gotResp.Sig)
		require.Equal(t, resp.canonicalBytes(), gotResp.canonicalBytes())
	}
}

// TestPQBody_NoTruncatedPrefixDecodes is the classic framing property: a
// short read must never produce a structurally valid message. Every prefix of
// a real INIT is refused, so a peer cannot get a handshake half-built out of
// bytes it stopped sending.
func TestPQBody_NoTruncatedPrefixDecodes(t *testing.T) {
	init, resp, _ := newWireMessages(t, kem.KeyExchangeMLKEM768)

	whole := init.canonicalBytes()
	for cut := 0; cut < len(whole); cut++ {
		got, err := parsePQHandshakeInit(whole[:cut], kem.KeyExchangeMLKEM768)
		require.Nil(t, got, "INIT prefix of %d/%d bytes decoded", cut, len(whole))
		require.Error(t, err, "INIT prefix of %d/%d bytes decoded", cut, len(whole))
	}
	require.NotNil(t, must[*HandshakeInit](t)(parsePQHandshakeInit(whole, kem.KeyExchangeMLKEM768)))

	whole = resp.canonicalBytes()
	for cut := 0; cut < len(whole); cut++ {
		got, err := parsePQHandshakeResp(whole[:cut], kem.KeyExchangeMLKEM768)
		require.Nil(t, got, "RESP prefix of %d/%d bytes decoded", cut, len(whole))
		require.Error(t, err, "RESP prefix of %d/%d bytes decoded", cut, len(whole))
	}
}

// TestPQBody_TrailingBytesRefused closes the appendix channel: a message that
// carries anything after its last declared field is refused outright rather
// than parsed-and-ignored, so two peers can never disagree about what was
// signed while both call the frame well-formed.
func TestPQBody_TrailingBytesRefused(t *testing.T) {
	init, resp, _ := newWireMessages(t, kem.KeyExchangeMLKEM768)

	for _, extra := range [][]byte{{0x00}, {0xff}, bytes.Repeat([]byte{0x41}, 64)} {
		_, err := parsePQHandshakeInit(append(init.canonicalBytes(), extra...), kem.KeyExchangeMLKEM768)
		require.ErrorIs(t, err, ErrHandshakeBadIdentity,
			"INIT with %d trailing bytes must be refused", len(extra))

		_, err = parsePQHandshakeResp(append(resp.canonicalBytes(), extra...), kem.KeyExchangeMLKEM768)
		require.ErrorIs(t, err, ErrHandshakeBadIdentity,
			"RESP with %d trailing bytes must be refused", len(extra))
	}
}

// TestPQBody_LengthPrefixCannotOverrunTheFrame is the inner allocation bound.
// Each variable field carries its own peer-controlled 4-byte length; a parser
// that trusted it would allocate from a number rather than from bytes it
// actually holds. Overstate any one of them and the parse must fail.
func TestPQBody_LengthPrefixCannotOverrunTheFrame(t *testing.T) {
	init, _, _ := newWireMessages(t, kem.KeyExchangeMLKEM768)
	whole := init.canonicalBytes()

	// Fixed header: version(1) profile(1) chain(32) kem(1) nodeID(20) = 55,
	// then the MLDSAPub length prefix.
	const mldsaLenOffset = 1 + 1 + chainIDSize + 1 + nodeIDSize
	require.Equal(t, uint32(IdentityPublicKeySize),
		binary.BigEndian.Uint32(whole[mldsaLenOffset:]),
		"precondition: the first variable field is the ML-DSA public key")

	for _, claim := range []uint32{
		uint32(len(whole)),      // exactly the whole frame, still an overrun
		uint32(len(whole)) + 1,  //
		1 << 20,                 //
		^uint32(0),              // 4 GiB - 1: the number a hostile peer picks
	} {
		mangled := bytes.Clone(whole)
		binary.BigEndian.PutUint32(mangled[mldsaLenOffset:], claim)

		got, err := parsePQHandshakeInit(mangled, kem.KeyExchangeMLKEM768)
		require.Nil(t, got, "claim=%d", claim)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF,
			"claim=%d: a length from a peer must be bounded by the bytes it actually sent", claim)
	}

	// Understating the length is refused too, by the trailing-bytes rule: the
	// leftovers have nowhere to go.
	short := bytes.Clone(whole)
	binary.BigEndian.PutUint32(short[mldsaLenOffset:], IdentityPublicKeySize-1)
	_, err := parsePQHandshakeInit(short, kem.KeyExchangeMLKEM768)
	require.Error(t, err, "an understated field length must not silently re-frame the rest")
}

// TestPQBody_SchemeMismatchRefusedBeforeSignatureWork pins the ordering the
// comment on parsePQHandshakeInit promises: the KEM byte is checked while the
// signature is still unread, so a peer cannot make the verifier burn ML-DSA
// verifications by offering a scheme we do not run.
func TestPQBody_SchemeMismatchRefusedBeforeSignatureWork(t *testing.T) {
	init, resp, _ := newWireMessages(t, kem.KeyExchangeMLKEM768)

	// Truncate everything after the KEM byte. If the scheme check ran late,
	// this would fail with a truncation error instead.
	const kemByteOffset = 1 + 1 + chainIDSize
	stub := bytes.Clone(init.canonicalBytes()[:kemByteOffset+1])

	_, err := parsePQHandshakeInit(stub, kem.KeyExchangeMLKEM1024)
	require.ErrorIs(t, err, ErrHandshakeKEMScheme,
		"the offered KEM scheme must be refused before the message body is read")

	// And on a whole, otherwise valid message.
	_, err = parsePQHandshakeInit(init.canonicalBytes(), kem.KeyExchangeMLKEM1024)
	require.ErrorIs(t, err, ErrHandshakeKEMScheme)
	_, err = parsePQHandshakeResp(resp.canonicalBytes(), kem.KeyExchangeMLKEM1024)
	require.ErrorIs(t, err, ErrHandshakeKEMScheme)

	// A scheme byte this build has never heard of is refused the same way —
	// unknown is not "probably fine".
	unknown := bytes.Clone(init.canonicalBytes())
	unknown[kemByteOffset] = 0x7f
	_, err = parsePQHandshakeInit(unknown, kem.KeyExchangeMLKEM768)
	require.ErrorIs(t, err, ErrHandshakeKEMScheme)
}

// TestPQBody_ReflectedInitIsNotAValidResp is the role-confusion property, and
// ML-KEM-1024 is where it bites: the KEM public key and the ciphertext are
// both 1568 bytes, so an INIT is a structurally perfect RESP. Nothing about
// its shape says which side sent it. Only the signature context and the
// transcript do — and they must.
func TestPQBody_ReflectedInitIsNotAValidResp(t *testing.T) {
	require := require.New(t)
	initiator, _, chainID := newTestIdentities(t)
	cfg := pqTestConfig(chainID, kem.KeyExchangeMLKEM1024)

	init, kemSec, err := InitiateHandshake(cfg, initiator)
	require.NoError(err)

	kemPubSize, err := kem.PublicKeySize(kem.KeyExchangeMLKEM1024)
	require.NoError(err)
	ctSize, err := kem.CiphertextSize(kem.KeyExchangeMLKEM1024)
	require.NoError(err)
	require.Equal(kemPubSize, ctSize,
		"precondition: on ML-KEM-1024 an INIT and a RESP are indistinguishable by length")

	// Bounce the initiator's own INIT back at it, relabelled as a RESP. It
	// parses — that is the point — and every length check passes.
	reflected, err := parsePQHandshakeResp(init.canonicalBytes(), kem.KeyExchangeMLKEM1024)
	require.NoError(err, "the reflected frame is structurally a RESP")
	require.NoError(validateRemoteResp(cfg, init, reflected),
		"and it survives every cross-axis size and value check")

	// The signature is what refuses it: it was made under the initiator
	// context over the initiator prefix, and cannot be replayed as a
	// responder's.
	_, err = FinishInitiatorHandshake(cfg, initiator, init, reflected, kemSec)
	require.ErrorIs(err, ErrHandshakeBadIdentity,
		"an INIT reflected as a RESP must be refused by the role-bound signature")
}

// TestPQReader_FixedFieldsRespectTheirWidth exercises the reader primitives at
// their edges directly: a fixed field one byte short of its width must not be
// zero-padded into a different value.
func TestPQReader_FixedFieldsRespectTheirWidth(t *testing.T) {
	require := require.New(t)

	r := newPQReader(nil)
	require.Equal(0, r.remaining())
	_, err := r.readU8()
	require.ErrorIs(err, io.ErrUnexpectedEOF)
	require.ErrorIs(r.readFixed(make([]byte, 1)), io.ErrUnexpectedEOF)
	_, err = r.readBytes()
	require.ErrorIs(err, io.ErrUnexpectedEOF)

	// 31 bytes offered for a 32-byte chain id: refuse, do not pad.
	r = newPQReader(bytes.Repeat([]byte{0xEE}, chainIDSize-1))
	require.ErrorIs(r.readFixed(make([]byte, chainIDSize)), io.ErrUnexpectedEOF)
	require.Equal(chainIDSize-1, r.remaining(),
		"a refused read must not advance the cursor")

	// A length prefix with no bytes behind it.
	r = newPQReader(header(1))
	_, err = r.readBytes()
	require.ErrorIs(err, io.ErrUnexpectedEOF)

	// Zero-length field is legal and consumes only its prefix.
	r = newPQReader(append(header(0), 0x99))
	b, err := r.readBytes()
	require.NoError(err)
	require.Empty(b)
	require.Equal(1, r.remaining())
}

// TestPQBody_NodeIDZeroSurvivesParsingAndDiesAtValidation separates the two
// jobs: the parser reports what arrived, the validator decides what is
// allowed. A zero NodeID is well-formed on the wire and inadmissible in a
// handshake, and conflating the two would let a policy change silently become
// a parse change.
func TestPQBody_NodeIDZeroSurvivesParsingAndDiesAtValidation(t *testing.T) {
	require := require.New(t)
	init, _, cfg := newWireMessages(t, kem.KeyExchangeMLKEM768)

	const nodeIDOffset = 1 + 1 + chainIDSize + 1
	zeroed := bytes.Clone(init.canonicalBytes())
	copy(zeroed[nodeIDOffset:nodeIDOffset+nodeIDSize], make([]byte, nodeIDSize))

	parsed, err := parsePQHandshakeInit(zeroed, kem.KeyExchangeMLKEM768)
	require.NoError(err, "the parser reports what arrived")
	require.Equal(ids.EmptyNodeID, parsed.NodeID)

	require.ErrorIs(validateRemoteInit(cfg, parsed), ErrHandshakeNodeIDZero,
		"the validator refuses the zero NodeID")
}

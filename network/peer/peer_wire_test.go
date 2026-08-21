// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/node/utils/bloom"
	"github.com/luxfi/node/version"
)

// A peer's first duty is to survive whoever dialled it. Everything below
// drives one end of a real connection by hand — framing bytes the way a node
// on the far side of the internet would — and asks one question of each
// malformed handshake: does this peer stop talking to it.
//
// The positive control matters as much as the refusals. A harness that fails
// to deliver anything would show every hostile case "passing" while the peer
// was never told a thing; TestWire_WellFormedHandshakeIsAccepted is what says
// the bytes are arriving.

// wirePeer is the hand-driven side of a peer connection: the identity, the
// signed address claim and the socket a hostile node would have.
type wirePeer struct {
	raw      *rawTestPeer
	conn     net.Conn
	mc       message.Creator
	signedIP *SignedIP
}

// startWirePair stands up a real peer against a hand-driven connection. The
// returned Peer is under test; the wirePeer is the attacker.
func startWirePair(t *testing.T) (Peer, *wirePeer) {
	t.Helper()
	require := require.New(t)

	victimCfg := newConfig(t)
	// A node that states no chains never compares any, so the chain-identity
	// checks would be dead code in this harness without it.
	victimCfg.MyChainIdentities = &Chains{}

	victimRaw := newRawTestPeer(t, victimCfg)
	attackerRaw := newRawTestPeer(t, newConfig(t))

	signedIP, err := attackerRaw.config.IPSigner.GetSignedIP()
	require.NoError(err)

	victimConn, attackerConn := net.Pipe()
	victim := Start(
		victimRaw.config,
		victimConn,
		attackerRaw.cert,
		attackerRaw.config.MyNodeID,
		NewThrottledMessageQueue(
			victimRaw.config.Metrics,
			attackerRaw.config.MyNodeID,
			log.NewNoOpLogger(),
			throttling.NewNoOutboundThrottler(),
		),
		false,
		nil,
	)
	t.Cleanup(func() {
		victim.StartClose()
		_ = attackerConn.Close()
	})

	attacker := &wirePeer{
		raw:      attackerRaw,
		conn:     attackerConn,
		mc:       attackerRaw.config.MessageCreator,
		signedIP: signedIP,
	}
	// The victim writes its own handshake immediately and net.Pipe is
	// unbuffered, so somebody has to be reading or the victim blocks before
	// it ever reads ours.
	go attacker.drain()
	return victim, attacker
}

// drain reads and discards whatever the peer sends, so its writer goroutine
// never blocks on an unbuffered pipe.
func (w *wirePeer) drain() {
	header := make([]byte, 4)
	for {
		if _, err := io.ReadFull(w.conn, header); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(header)
		if n > constants.DefaultMaxMessageSize {
			return
		}
		if _, err := io.CopyN(io.Discard, w.conn, int64(n)); err != nil {
			return
		}
	}
}

// send frames one message onto the wire exactly as the peer's own writer does:
// a 4-byte big-endian length followed by the message bytes.
func (w *wirePeer) send(t *testing.T, msg message.OutboundMessage) {
	t.Helper()
	body := msg.Bytes()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := w.conn.Write(header[:]); err != nil {
		return // the peer already hung up, which is an answer in itself
	}
	_, _ = w.conn.Write(body)
}

// handshake is the message a well-behaved peer would send. Each test mutates
// exactly one field of it, so a refusal names one cause.
type handshake struct {
	networkID     uint32
	myTime        uint64
	ipSigningTime uint64
	tlsSig        []byte
	blsSig        []byte
	mldsaSig      []byte
	addr          []byte
	port          uint32
	trackedNets   [][]byte
	supportedLPs  []uint32
	objectedLPs   []uint32
	filter        []byte
	salt          []byte
	chains        []*p2p.ChainIdentity
	client        *p2p.Client
}

func (w *wirePeer) validHandshake() *handshake {
	now := uint64(time.Now().Unix())
	v := version.CurrentApp
	return &handshake{
		networkID:     constants.LocalID,
		myTime:        now,
		ipSigningTime: w.signedIP.Timestamp,
		tlsSig:        w.signedIP.TLSSignature,
		blsSig:        w.signedIP.BLSSignatureBytes,
		mldsaSig:      w.signedIP.MLDSASignature,
		addr:          w.signedIP.AddrPort.Addr().AsSlice(),
		port:          uint32(w.signedIP.AddrPort.Port()),
		filter:        bloom.EmptyFilter.Marshal(),
		client: &p2p.Client{
			Name:  v.Name,
			Major: uint32(v.Major),
			Minor: uint32(v.Minor),
			Patch: uint32(v.Patch),
		},
	}
}

// sendHandshake serialises h with the raw builder so a test can put values on
// the wire that the typed constructor would not let it build.
func (w *wirePeer) sendHandshake(t *testing.T, h *handshake) {
	t.Helper()
	msg, err := w.mc.Handshake(
		h.networkID,
		h.myTime,
		w.signedIP.AddrPort, // overwritten below
		h.client.GetName(),
		h.client.GetMajor(),
		h.client.GetMinor(),
		h.client.GetPatch(),
		h.ipSigningTime,
		h.tlsSig,
		h.blsSig,
		nil,
		h.supportedLPs,
		h.objectedLPs,
		h.filter,
		h.salt,
		false,
		h.mldsaSig,
		h.chains,
	)
	require.NoError(t, err)

	// Re-encode with the address, port and tracked-net bytes the test asked
	// for. These are the fields a peer controls verbatim on the wire and the
	// typed constructor cannot express.
	inbound := decodeHandshake(t, msg.Bytes())
	inbound.IpAddr = h.addr
	inbound.IpPort = h.port
	inbound.TrackedNets = h.trackedNets

	w.send(t, encodeHandshake(t, w.mc, inbound))
}

// decodeHandshake pulls the Handshake back out of an encoded outbound message
// so a test can edit fields the builder does not expose.
func decodeHandshake(t *testing.T, b []byte) *p2p.Handshake {
	t.Helper()
	var msg p2p.Message
	require.NoError(t, p2p.Unmarshal(b, &msg))
	hs := msg.GetHandshake()
	require.NotNil(t, hs, "expected a Handshake on the wire")
	return hs
}

// encodeHandshake re-wraps an edited Handshake as an outbound message.
func encodeHandshake(t *testing.T, mc message.Creator, hs *p2p.Handshake) message.OutboundMessage {
	t.Helper()
	b, err := p2p.Marshal(&p2p.Message{Message: &p2p.Message_Handshake{Handshake: hs}})
	require.NoError(t, err)
	return rawOutbound(b)
}

// rawOutbound is an already-encoded message, so a test can put bytes on the
// wire that no builder would produce.
type rawOutbound []byte

func (rawOutbound) BypassThrottling() bool     { return true }
func (rawOutbound) Op() message.Op             { return message.HandshakeOp }
func (m rawOutbound) Bytes() []byte            { return m }
func (rawOutbound) BytesSavedCompression() int { return 0 }

// awaitClosed asserts the peer hung up, which is the only sanction it has
// against a peer that sent something it must not have.
func awaitClosed(t *testing.T, p Peer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.AwaitClosed(ctx), "the peer must disconnect")
	require.True(t, p.Closed())
}

// awaitRefusedHandshake is the stronger sanction for the handshake phase: the
// peer must hang up AND must never have counted the connection as usable, so
// nothing downstream ever saw this peer as a participant.
func awaitRefusedHandshake(t *testing.T, p Peer) {
	t.Helper()
	awaitClosed(t, p)
	require.False(t, p.Ready(), "a refused handshake must never leave the peer ready")
}

// TestWire_WellFormedHandshakeIsAccepted is the positive control for every
// refusal below. Without it, a harness that silently delivered nothing would
// make each hostile case look like a catch.
func TestWire_WellFormedHandshakeIsAccepted(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	attacker.sendHandshake(t, attacker.validHandshake())

	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx), "a well-formed handshake must be accepted")
	require.True(victim.Ready())
}

// TestWire_HandshakeRefusals walks every field of the handshake a peer
// controls and requires that a value outside the contract ends the
// connection. One case per field, so a refusal that stops working names
// exactly which check went missing.
func TestWire_HandshakeRefusals(t *testing.T) {
	cases := map[string]func(*wirePeer, *handshake){
		// The network id is the first thing that must match: a mainnet node
		// and a testnet node sharing a connection is a fork waiting to be
		// reported as a bug in consensus.
		"another network": func(_ *wirePeer, h *handshake) {
			h.networkID = constants.LocalID + 1
		},

		// Clock skew past the configured window, both directions. A peer
		// whose clock is far off cannot be reasoned about: every timestamp
		// it signs is either already expired or valid for far too long.
		"clock far ahead": func(_ *wirePeer, h *handshake) {
			h.myTime = uint64(time.Now().Add(time.Hour).Unix())
		},
		"clock far behind": func(_ *wirePeer, h *handshake) {
			h.myTime = uint64(time.Now().Add(-time.Hour).Unix())
		},

		// The address claim. Anything that is not 4 or 16 bytes is not an
		// address, and a zero port is not somewhere you can dial back.
		"address of the wrong width": func(_ *wirePeer, h *handshake) {
			h.addr = []byte{1, 2, 3}
		},
		"address is empty": func(_ *wirePeer, h *handshake) {
			h.addr = nil
		},
		"port zero": func(_ *wirePeer, h *handshake) {
			h.port = 0
		},
		"port above the wire width": func(_ *wirePeer, h *handshake) {
			h.port = 1 << 16
		},

		// The signature over the address claim. Without these the address is
		// whatever the sender felt like typing, and a peer could point the
		// network's gossip at anyone.
		"address signature is garbage": func(_ *wirePeer, h *handshake) {
			h.tlsSig = []byte("not a signature")
		},
		"address signature is absent": func(_ *wirePeer, h *handshake) {
			h.tlsSig = nil
		},
		"address signature flipped": func(_ *wirePeer, h *handshake) {
			edited := append([]byte(nil), h.tlsSig...)
			edited[len(edited)/2] ^= 0x01
			h.tlsSig = edited
		},
		"address signed for a different port": func(w *wirePeer, h *handshake) {
			h.port = uint32(w.signedIP.AddrPort.Port()) + 1
		},
		"address signing time moved": func(_ *wirePeer, h *handshake) {
			h.ipSigningTime++
		},
		"address claim from the deep past": func(w *wirePeer, h *handshake) {
			h.ipSigningTime = w.signedIP.Timestamp - uint64(time.Hour.Seconds())
		},

		// The BLS leg has to parse before anyone can check it.
		"bls signature is garbage": func(_ *wirePeer, h *handshake) {
			h.blsSig = []byte{0xde, 0xad, 0xbe, 0xef}
		},

		// The bloom filter sizes a lookup structure from bytes a peer chose.
		"bloom filter is garbage": func(_ *wirePeer, h *handshake) {
			h.filter = []byte{0xff, 0xff, 0xff, 0xff}
		},
		"bloom salt is oversized": func(_ *wirePeer, h *handshake) {
			h.salt = make([]byte, maxBloomSaltLen+1)
		},

		// Tracked chain ids arrive as raw bytes and are widened into ids; a
		// short one would pad into a chain nobody is running.
		"tracked chain of the wrong width": func(_ *wirePeer, h *handshake) {
			h.trackedNets = [][]byte{{1, 2, 3}}
		},
		"too many tracked chains": func(_ *wirePeer, h *handshake) {
			h.trackedNets = make([][]byte, maxNumTrackedChains+1)
			for i := range h.trackedNets {
				id := ids.GenerateTestID()
				h.trackedNets[i] = id[:]
			}
		},

		// Chain identities are the per-chain "which chain do you mean"
		// statement. A field of the wrong width is malformed, not a
		// disagreement, and must not be read as one.
		"chain identity with a short genesis digest": func(_ *wirePeer, h *handshake) {
			id := ids.GenerateTestID()
			h.chains = []*p2p.ChainIdentity{{
				NetworkId:     constants.LocalID,
				ChainId:       id[:],
				VmId:          id[:],
				GenesisDigest: []byte{1, 2, 3},
			}}
		},
		"chain identity with a short chain id": func(_ *wirePeer, h *handshake) {
			id := ids.GenerateTestID()
			h.chains = []*p2p.ChainIdentity{{
				NetworkId:     constants.LocalID,
				ChainId:       []byte{1, 2, 3},
				VmId:          id[:],
				GenesisDigest: id[:],
			}}
		},
		"chain identity with a short rules id": func(_ *wirePeer, h *handshake) {
			id := ids.GenerateTestID()
			h.chains = []*p2p.ChainIdentity{{
				NetworkId:     constants.LocalID,
				ChainId:       id[:],
				VmId:          id[:],
				GenesisDigest: id[:],
				RulesId:       []byte{9},
			}}
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			victim, attacker := startWirePair(t)
			h := attacker.validHandshake()
			mutate(attacker, h)
			attacker.sendHandshake(t, h)
			awaitRefusedHandshake(t, victim)
		})
	}
}

// TestWire_SecondHandshakeIsRefused closes the re-handshake channel. The
// handshake sets the peer's version, address claim, tracked chains and chain
// verdicts in one shot; letting a second one land would let a peer rewrite any
// of them after it had already been accepted, which is a whole class of bug
// nobody would find twice.
func TestWire_SecondHandshakeIsRefused(t *testing.T) {
	victim, attacker := startWirePair(t)

	attacker.sendHandshake(t, attacker.validHandshake())
	attacker.sendHandshake(t, attacker.validHandshake())

	awaitRefusedHandshake(t, victim)
}

// TestWire_UptimeAboveOneHundredPercentIsRefused. Uptime is a percentage the
// peer reports about US, and it feeds reward accounting. Above 100 is not a
// number this protocol has a meaning for, and accepting it would mean the
// bound is enforced somewhere downstream or nowhere at all.
func TestWire_UptimeAboveOneHundredPercentIsRefused(t *testing.T) {
	require := require.New(t)

	for _, uptime := range []uint32{101, 1 << 20, ^uint32(0)} {
		victim, attacker := startWirePair(t)
		attacker.sendHandshake(t, attacker.validHandshake())

		ping, err := attacker.mc.Ping(uptime)
		require.NoError(err)
		attacker.send(t, ping)

		awaitClosed(t, victim)
	}
}

// TestWire_UptimeAtTheBoundIsAccepted is the other half of the same rule: 100
// is a legal reading, and a bound that refused it would silently drop every
// report from a perfectly healthy node.
func TestWire_UptimeAtTheBoundIsAccepted(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	attacker.sendHandshake(t, attacker.validHandshake())
	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx))

	ping, err := attacker.mc.Ping(100)
	require.NoError(err)
	attacker.send(t, ping)

	// Nothing to await but the absence of a disconnect: give the reader a
	// moment and require the peer is still up.
	time.Sleep(200 * time.Millisecond)
	require.True(victim.Ready(), "100%% uptime is a legal report")
	require.EqualValues(100, victim.ObservedUptime())
}

// TestWire_PeerListBeforeHandshakeIsIgnored pins the ordering rule. A peer
// that has not identified itself has not earned the right to seed our address
// book, and the message must be dropped rather than acted on — but it is not
// itself hostile, so the connection stays up until the handshake decides.
func TestWire_PeerListBeforeHandshakeIsIgnored(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	time.Sleep(200 * time.Millisecond)
	require.False(victim.Ready(), "a peer list cannot complete a handshake that never happened")

	// The handshake still works afterwards, so the early message was ignored
	// and not treated as the handshake's peer list.
	attacker.sendHandshake(t, attacker.validHandshake())
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx))
}

// TestWire_PeerListWithAMalformedClaimIsRefused. A peer list is the address
// book seed; a claim in it that does not parse is a peer trying to write junk
// into everyone's dial list, and the whole message must be refused rather than
// partially absorbed.
func TestWire_PeerListWithAMalformedClaimIsRefused(t *testing.T) {
	cases := map[string]*p2p.ClaimedIpPort{
		"certificate is not a certificate": {
			X509Certificate: []byte("nope"),
			IpAddr:          []byte{127, 0, 0, 1},
			IpPort:          9651,
			Timestamp:       uint64(time.Now().Unix()),
		},
		"certificate is absent": {
			IpAddr:    []byte{127, 0, 0, 1},
			IpPort:    9651,
			Timestamp: uint64(time.Now().Unix()),
		},
	}

	for name, claim := range cases {
		t.Run(name, func(t *testing.T) {
			victim, attacker := startWirePair(t)
			attacker.sendHandshake(t, attacker.validHandshake())

			b, err := p2p.Marshal(&p2p.Message{
				Message: &p2p.Message_PeerList_{
					PeerList_: &p2p.PeerList{ClaimedIpPorts: []*p2p.ClaimedIpPort{claim}},
				},
			})
			require.NoError(t, err)
			attacker.send(t, rawOutbound(b))

			awaitClosed(t, victim)
		})
	}
}

// TestWire_PeerListClaimWithABadAddressIsRefused reaches the address and port
// checks inside the peer list, which are the same checks the handshake applies
// to the sender's own claim. They have to hold in both places: a peer that
// cannot lie about its own address but can lie about everyone else's has not
// been constrained at all.
func TestWire_PeerListClaimWithABadAddressIsRefused(t *testing.T) {
	base := func(t *testing.T, attacker *wirePeer) *p2p.ClaimedIpPort {
		t.Helper()
		return &p2p.ClaimedIpPort{
			X509Certificate: attacker.raw.cert.Raw,
			IpAddr:          []byte{127, 0, 0, 1},
			IpPort:          9651,
			Timestamp:       uint64(time.Now().Unix()),
			Signature:       []byte{1, 2, 3},
		}
	}

	cases := map[string]func(*p2p.ClaimedIpPort){
		"address of the wrong width": func(c *p2p.ClaimedIpPort) { c.IpAddr = []byte{1, 2, 3} },
		"address is empty":           func(c *p2p.ClaimedIpPort) { c.IpAddr = nil },
		"port zero":                  func(c *p2p.ClaimedIpPort) { c.IpPort = 0 },
		"port above the wire width":  func(c *p2p.ClaimedIpPort) { c.IpPort = 1 << 16 },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			victim, attacker := startWirePair(t)
			attacker.sendHandshake(t, attacker.validHandshake())

			claim := base(t, attacker)
			mutate(claim)
			b, err := p2p.Marshal(&p2p.Message{
				Message: &p2p.Message_PeerList_{
					PeerList_: &p2p.PeerList{ClaimedIpPorts: []*p2p.ClaimedIpPort{claim}},
				},
			})
			require.NoError(t, err)
			attacker.send(t, rawOutbound(b))

			awaitClosed(t, victim)
		})
	}
}

// TestWire_GetPeerListWithAMalformedFilterIsRefused. The known-peers filter
// and its salt are peer-supplied inputs to a lookup structure; a filter that
// does not parse and a salt past its bound are both refused, on this message
// as on the handshake.
func TestWire_GetPeerListWithAMalformedFilterIsRefused(t *testing.T) {
	cases := map[string]*p2p.BloomFilter{
		"filter is garbage":   {Filter: []byte{0xff, 0xff, 0xff, 0xff}},
		"filter is absent":    {},
		"salt is oversized":   {Filter: bloom.EmptyFilter.Marshal(), Salt: make([]byte, maxBloomSaltLen+1)},
	}

	for name, known := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			victim, attacker := startWirePair(t)

			attacker.sendHandshake(t, attacker.validHandshake())
			peerList, err := attacker.mc.PeerList(nil, true)
			require.NoError(err)
			attacker.send(t, peerList)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(victim.AwaitReady(ctx), "precondition: the peer is up before we misbehave")

			b, err := p2p.Marshal(&p2p.Message{
				Message: &p2p.Message_GetPeerList{
					GetPeerList: &p2p.GetPeerList{KnownPeers: known},
				},
			})
			require.NoError(err)
			attacker.send(t, rawOutbound(b))

			awaitClosed(t, victim)
		})
	}
}

// TestWire_OversizedFrameIsRefused is the read-side allocation bound. The
// length prefix is four bytes a peer picks freely; a reader that believed it
// would size a buffer from a number rather than from bytes it holds. The peer
// must hang up on the header alone, without waiting for a body that is never
// coming.
func TestWire_OversizedFrameIsRefused(t *testing.T) {
	for _, announced := range []uint32{
		constants.DefaultMaxMessageSize + 1,
		1 << 30,
		^uint32(0),
	} {
		victim, attacker := startWirePair(t)

		var header [4]byte
		binary.BigEndian.PutUint32(header[:], announced)
		_, _ = attacker.conn.Write(header[:])

		awaitClosed(t, victim)
	}
}

// TestWire_TruncatedFrameIsRefused. A peer that announces a length and then
// stops must not leave the reader holding a half-message, and must not leave
// it holding the connection either.
func TestWire_TruncatedFrameIsRefused(t *testing.T) {
	victim, attacker := startWirePair(t)

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 4096)
	_, _ = attacker.conn.Write(header[:])
	_, _ = attacker.conn.Write([]byte{1, 2, 3})
	_ = attacker.conn.Close()

	awaitClosed(t, victim)
}

// TestWire_UndecodableMessageIsDroppedNotGuessedAt pins the tolerance the
// reader deliberately has for garbage: a frame that is well formed but is not
// a protocol message is counted and skipped, and the connection survives —
// a stray corrupt frame must not cost a peer its link.
//
// What it must NOT do is advance any state. The peer that sent it has still
// not handshaked, and a well-formed handshake afterwards has to work, which is
// how we know the garbage was dropped rather than half-absorbed.
func TestWire_UndecodableMessageIsDroppedNotGuessedAt(t *testing.T) {
	require := require.New(t)
	victim, attacker := startWirePair(t)

	for _, garbage := range [][]byte{
		{0xff, 0xfe, 0xfd, 0xfc, 0xfb},
		{0x00},
		make([]byte, 64),
		[]byte("this is not a p2p message"),
	} {
		attacker.send(t, rawOutbound(garbage))
	}

	time.Sleep(200 * time.Millisecond)
	require.False(victim.Ready(), "garbage cannot complete a handshake")
	require.False(victim.Closed(), "a corrupt frame must not cost a peer its connection")

	attacker.sendHandshake(t, attacker.validHandshake())
	peerList, err := attacker.mc.PeerList(nil, true)
	require.NoError(err)
	attacker.send(t, peerList)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(victim.AwaitReady(ctx),
		"the reader must resynchronise: garbage before a handshake cannot poison it")
}

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/version"
)

// newHandlerPeer builds a peer with exactly the fields Start assigns, minus the
// three goroutines, so a handler can be called on the test's own goroutine.
// Calling it here rather than over a socket is what lets a panic be reported as
// a failure instead of taking the whole test binary down with it — which is
// itself the shape of the defect below.
func newHandlerPeer(t *testing.T) (*peer, *staking.Certificate, *SignedIP) {
	t.Helper()
	require := require.New(t)

	self := newRawTestPeer(t, newConfig(t))
	remote := newRawTestPeer(t, newConfig(t))
	signedIP, err := remote.config.IPSigner.GetSignedIP()
	require.NoError(err)

	localConn, remoteConn := net.Pipe()
	t.Cleanup(func() {
		_ = localConn.Close()
		_ = remoteConn.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Field for field with Start: anything initialised here that Start leaves
	// alone would hide the defect rather than find it.
	p := &peer{
		Config:             self.config,
		conn:               localConn,
		cert:               remote.cert,
		id:                 remote.config.MyNodeID,
		messageQueue:       NewBlockingMessageQueue(self.config.Metrics, self.config.Log, 16),
		onFinishHandshake:  make(chan struct{}),
		numExecuting:       3,
		onClosingCtx:       ctx,
		onClosingCtxCancel: cancel,
		onClosed:           make(chan struct{}),
		getPeerListChan:    make(chan struct{}, 1),
		trackedChains:      make(set.Set[ids.ID]),
	}
	return p, remote.cert, signedIP
}

// validHandshakeMsg is the message a well-behaved peer sends, built as the
// wire struct so a test can set the fields the typed builder does not reach.
func validHandshakeMsg(signedIP *SignedIP) *p2p.Handshake {
	v := version.CurrentApp
	return &p2p.Handshake{
		NetworkId:     constants.LocalID,
		MyTime:        uint64(time.Now().Unix()),
		IpAddr:        signedIP.AddrPort.Addr().AsSlice(),
		IpPort:        uint32(signedIP.AddrPort.Port()),
		IpSigningTime: signedIP.Timestamp,
		IpNodeIdSig:   signedIP.TLSSignature,
		IpBlsSig:      signedIP.BLSSignatureBytes,
		IpMldsaSig:    signedIP.MLDSASignature,
		Client: &p2p.Client{
			Name:  v.Name,
			Major: uint32(v.Major),
			Minor: uint32(v.Minor),
			Patch: uint32(v.Patch),
		},
	}
}

// TestHandshake_ProtocolOpinionMustNotCrashTheNode.
//
// A handshake carries the peer's opinions about pending protocol changes:
// which it supports, which it objects to. The handler files each recognised
// one into a set on the peer:
//
//	if constants.CurrentLPs.Contains(lp) { p.supportedLPs.Add(lp) }
//
// Those two sets are never made. Start initialises trackedChains and stops
// there, so supportedLPs and objectedLPs are nil maps and Add on a nil set
// panics. The panic runs on the peer's reader goroutine, where no recover
// stands between it and the process.
//
// Nothing authenticates the opinion — it is a repeated field in the first
// message a peer sends — so any node that finishes the TLS upgrade can name a
// live protocol number and take the process down. It has stayed quiet only
// because honest nodes send the field empty: config unions the scheduled LPs
// into it and then subtracts the activated ones, and today those two sets are
// the same, so the list ships empty. The handler checks membership against
// CurrentLPs, which is NOT empty. One number is enough.
//
// The assertion is simply that the handler returns. It is called on the test's
// own goroutine because a panic on the reader goroutine cannot be recovered by
// anyone — which is the whole reason this matters.
func TestHandshake_ProtocolOpinionMustNotCrashTheNode(t *testing.T) {
	for name, opinion := range map[string]func(*p2p.Handshake){
		"supports a live protocol": func(h *p2p.Handshake) {
			h.SupportedLps = constants.CurrentLPs.List()
		},
		"objects to a live protocol": func(h *p2p.Handshake) {
			h.ObjectedLps = constants.CurrentLPs.List()
		},
		"both at once": func(h *p2p.Handshake) {
			h.SupportedLps = constants.CurrentLPs.List()
			h.ObjectedLps = constants.CurrentLPs.List()
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, constants.CurrentLPs.List(),
				"precondition: this build recognises at least one protocol number")

			p, _, signedIP := newHandlerPeer(t)
			msg := validHandshakeMsg(signedIP)
			opinion(msg)

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("a peer crashed the node by naming a protocol number in its handshake: %v", r)
				}
			}()
			p.handleHandshake(msg)
		})
	}
}

// TestHandshake_ProtocolOpinionIsRecorded is the other half of the same rule.
// Refusing to crash is not enough: the opinions the peer states have to be
// readable afterwards, or the field is decoration and the conflict check below
// it can never fire.
func TestHandshake_ProtocolOpinionIsRecorded(t *testing.T) {
	require := require.New(t)
	live := constants.CurrentLPs.List()
	require.NotEmpty(live, "precondition: this build recognises at least one protocol number")

	p, _, signedIP := newHandlerPeer(t)
	msg := validHandshakeMsg(signedIP)
	msg.SupportedLps = live
	// An unrecognised number is not an opinion this build can hold, and must
	// be dropped rather than stored.
	msg.ObjectedLps = []uint32{^uint32(0)}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleHandshake panicked: %v", r)
		}
	}()
	p.handleHandshake(msg)

	require.True(p.gotHandshake.Get(), "a well-formed handshake must be accepted")
	for _, lp := range live {
		require.Contains(p.supportedLPs, lp, "a stated opinion must be readable afterwards")
	}
	require.NotContains(p.objectedLPs, ^uint32(0),
		"an opinion about a protocol this build does not know is not an opinion")
}

// TestHandshake_ContradictoryProtocolOpinionIsRefused reaches the check that
// sits directly below the two Add calls. A peer that both supports and objects
// to the same change is trying to be counted on both sides; it must be
// disconnected. That check cannot run at all while filing the first opinion
// crashes the process, so this test only means anything once the sets exist.
func TestHandshake_ContradictoryProtocolOpinionIsRefused(t *testing.T) {
	require := require.New(t)
	live := constants.CurrentLPs.List()
	require.NotEmpty(live)

	p, _, signedIP := newHandlerPeer(t)
	msg := validHandshakeMsg(signedIP)
	msg.SupportedLps = live
	msg.ObjectedLps = live

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleHandshake panicked: %v", r)
		}
	}()
	p.handleHandshake(msg)

	require.False(p.gotHandshake.Get(),
		"a peer on both sides of a protocol change must not complete its handshake")
	select {
	case <-p.onClosingCtx.Done():
	default:
		t.Fatal("a peer on both sides of a protocol change must be disconnected")
	}
}

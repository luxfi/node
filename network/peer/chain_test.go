// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	zappb "github.com/luxfi/proto/node/zap/p2p"
)

// chain_test.go proves the two things that make this check safe rather than
// merely present:
//
//   - a peer that DECLARES a different chain is excluded from that chain, in
//     both directions, and left alone on every other chain;
//   - a peer that declares NOTHING is not excluded from anything, so a fleet
//     upgraded one node at a time never partitions itself.
//
// The second is the one that would turn this feature into an outage if it were
// wrong, so it is tested at every step of a real five-node rolling upgrade.

var (
	dChainID = ids.GenerateTestID()
	cChainID = ids.GenerateTestID()
	dVMID    = ids.GenerateTestID()

	// Two D-Chains created from different documents. G is what the fleet runs;
	// G' is one node handed the wrong creation record.
	genesisG      = GenesisDigest([]byte(`{"name":"D-Chain","networkId":1}`))
	genesisGPrime = GenesisDigest([]byte(`{"name":"D-Chain","networkId":2}`))

	dChainG = ChainIdentity{
		NetworkID: 1, ChainID: dChainID, VMID: dVMID, Genesis: genesisG,
	}
	dChainGPrime = ChainIdentity{
		NetworkID: 1, ChainID: dChainID, VMID: dVMID, Genesis: genesisGPrime,
	}
	cChain = ChainIdentity{
		NetworkID: 1, ChainID: cChainID, VMID: ids.GenerateTestID(),
		Genesis: GenesisDigest([]byte(`{"name":"C-Chain"}`)),
	}
)

// newNode builds a node that states the given chains. A node given none states
// nothing and compares nothing — which is exactly a node that predates the
// field, and is how the legacy side of a mixed fleet is modelled.
func newNode(t *testing.T, chains ...ChainIdentity) *rawTestPeer {
	t.Helper()
	config := newConfig(t)
	if len(chains) > 0 {
		config.MyChainIdentities = &Chains{}
		for _, c := range chains {
			config.MyChainIdentities.Add(c)
		}
	}
	return newRawTestPeer(t, config)
}

// connect runs a real handshake between two nodes over a pipe and returns each
// one's view of the other: left is a's view of b, right is b's view of a.
func connect(t *testing.T, a, b *rawTestPeer) (Peer, Peer) {
	t.Helper()
	peerA, peerB := startTestPeers(a, b)
	awaitReady(t, peerA, peerB)
	t.Cleanup(func() {
		peerA.StartClose()
		_ = peerA.AwaitClosed(context.Background())
		_ = peerB.AwaitClosed(context.Background())
	})
	return peerA, peerB
}

// TestChainIdentity_SameGenesisIsCompatible: the ordinary case. Two nodes on the
// same chain say so and neither excludes the other.
func TestChainIdentity_SameGenesisIsCompatible(t *testing.T) {
	require := require.New(t)

	a, b := newNode(t, dChainG, cChain), newNode(t, dChainG, cChain)
	viewOfB, viewOfA := connect(t, a, b)

	require.Equal(ChainCompatible, viewOfB.ChainState(dChainID))
	require.Equal(ChainCompatible, viewOfA.ChainState(dChainID))
	require.Equal(ChainCompatible, viewOfB.ChainState(cChainID))
}

// TestChainIdentity_DifferentGenesisIsIncompatible is the fork this exists to
// stop. Both nodes reach the verdict independently — neither is told by the
// other — and both keep the C-Chain, which they do agree on.
func TestChainIdentity_DifferentGenesisIsIncompatible(t *testing.T) {
	require := require.New(t)

	good := newNode(t, dChainG, cChain)
	bad := newNode(t, dChainGPrime, cChain)
	viewOfBad, viewOfGood := connect(t, good, bad)

	require.Equal(ChainIncompatible, viewOfBad.ChainState(dChainID),
		"a peer on a different D-Chain must be excluded from D")
	require.Equal(ChainIncompatible, viewOfGood.ChainState(dChainID),
		"the misconfigured node must reach the same verdict about the fleet")

	require.Equal(ChainCompatible, viewOfBad.ChainState(cChainID),
		"disagreeing about D must not cost them the C-Chain")
	require.Equal(ChainCompatible, viewOfGood.ChainState(cChainID))

	// And the connection itself survives: excluding a chain is not disconnecting
	// a peer. A node that dropped the connection would remove itself from every
	// chain over one chain's misconfiguration.
	require.True(viewOfBad.Ready())
	require.True(viewOfGood.Ready())
}

// TestChainIdentity_VMAndNetworkAlsoIsolate: genesis is the interesting field but
// it is not the only one that means "not the same chain".
func TestChainIdentity_VMAndNetworkAlsoIsolate(t *testing.T) {
	for name, theirs := range map[string]ChainIdentity{
		"different vm": {
			NetworkID: 1, ChainID: dChainID, VMID: ids.GenerateTestID(), Genesis: genesisG,
		},
		"different network": {
			NetworkID: 2, ChainID: dChainID, VMID: dVMID, Genesis: genesisG,
		},
	} {
		t.Run(name, func(t *testing.T) {
			viewOfThem, _ := connect(t, newNode(t, dChainG), newNode(t, theirs))
			require.Equal(t, ChainIncompatible, viewOfThem.ChainState(dChainID))
		})
	}
}

// TestChainIdentity_SilenceIsUnknownNotIncompatible. A node that says nothing is
// UNKNOWN, and unknown is permitted. This is the property that keeps the check
// from becoming the outage.
func TestChainIdentity_SilenceIsUnknownNotIncompatible(t *testing.T) {
	require := require.New(t)

	upgraded := newNode(t, dChainG)
	legacy := newNode(t) // states nothing, as a pre-upgrade node does

	viewOfLegacy, viewOfUpgraded := connect(t, upgraded, legacy)

	require.Equal(ChainUnknown, viewOfLegacy.ChainState(dChainID),
		"a peer that says nothing is unknown, never incompatible")
	require.Equal(ChainUnknown, viewOfUpgraded.ChainState(dChainID))
}

// TestChainIdentity_RollingUpgradeNeverPartitions walks the exact procedure used
// on a real fleet: five validators, upgraded one at a time, never more than one
// down at once because the remaining four carry quorum.
//
// At every step each upgraded node must still exchange D traffic with every
// other node, upgraded or not. If silence were treated as disagreement the first
// node upgraded would exclude the four still carrying consensus and isolate
// itself — the safety feature causing the outage it exists to prevent.
func TestChainIdentity_RollingUpgradeNeverPartitions(t *testing.T) {
	const fleet = 5

	for upgraded := 1; upgraded <= fleet; upgraded++ {
		t.Run(map[bool]string{true: "all upgraded", false: "mixed"}[upgraded == fleet], func(t *testing.T) {
			// Every ordered pair in the fleet, so this covers upgraded->legacy,
			// legacy->upgraded, and both same-kind pairs at every step.
			for i := 0; i < fleet; i++ {
				for j := i + 1; j < fleet; j++ {
					node := func(n int) *rawTestPeer {
						if n < upgraded {
							return newNode(t, dChainG)
						}
						return newNode(t)
					}
					viewOfJ, viewOfI := connect(t, node(i), node(j))

					wantState := ChainUnknown
					if i < upgraded && j < upgraded {
						wantState = ChainCompatible
					}
					require.Equal(t, wantState, viewOfJ.ChainState(dChainID),
						"%d upgraded: node %d must not exclude node %d", upgraded, i, j)
					require.Equal(t, wantState, viewOfI.ChainState(dChainID),
						"%d upgraded: node %d must not exclude node %d", upgraded, j, i)
					require.NotEqual(t, ChainIncompatible, viewOfJ.ChainState(dChainID),
						"a correctly configured fleet partitioned during a rolling upgrade")
				}
			}
		})
	}
}

// TestChainIdentity_QuarantineIsNotSticky. A node whose creation record is
// corrected must be readmitted when it reconnects. The verdict lives on the
// connection, so there is nothing keyed on nodeID to outlive the mistake — if
// there were, fixing the misconfiguration would not fix the exclusion and the
// operator would have no way to see that.
func TestChainIdentity_QuarantineIsNotSticky(t *testing.T) {
	require := require.New(t)

	good := newConfig(t)
	good.MyChainIdentities = &Chains{}
	good.MyChainIdentities.Add(dChainG)
	fleet := newRawTestPeer(t, good)

	// First connection: the peer is on the wrong chain.
	viewOfBad, _ := connect(t, fleet, newNode(t, dChainGPrime))
	require.Equal(ChainIncompatible, viewOfBad.ChainState(dChainID))

	// Its record is corrected and it reconnects — a fresh connection, judged
	// only by what is said on it.
	viewOfFixed, _ := connect(t, fleet, newNode(t, dChainG))
	require.Equal(ChainCompatible, viewOfFixed.ChainState(dChainID),
		"a corrected peer must be readmitted; the old verdict must not survive its connection")
}

// TestChainIdentity_ExclusionIsSymmetric. Inbound and outbound are decided by the
// same predicate. One-directional exclusion is worse than none: a node that
// stops listening while still talking wastes every request on a peer that will
// never answer, and one that keeps listening to a peer it has written off is
// still reachable by the blocks it declared it would not accept.
func TestChainIdentity_ExclusionIsSymmetric(t *testing.T) {
	require := require.New(t)

	good := newNode(t, dChainG, cChain)
	bad := newNode(t, dChainGPrime, cChain)
	viewOfBad, viewOfGood := connect(t, good, bad)

	for _, p := range []Peer{viewOfBad, viewOfGood} {
		require.Equal(ChainIncompatible, p.ChainState(dChainID))
	}

	// The inbound half, exercised end to end: a real D-Chain message from the
	// misconfigured peer must not reach the router, while the same message for
	// the C-Chain — which they agree on — must.
	mc := newMessageCreator(t)

	dMsg, err := mc.Get(dChainID, 1, time.Second, ids.GenerateTestID())
	require.NoError(err)
	require.True(viewOfGood.Send(context.Background(), dMsg))

	cMsg, err := mc.Get(cChainID, 2, time.Second, ids.GenerateTestID())
	require.NoError(err)
	require.True(viewOfGood.Send(context.Background(), cMsg))

	// Only the C-Chain message is delivered. Reading one message and finding it
	// is the C-Chain one proves the D-Chain message sent first was dropped
	// rather than merely delayed.
	select {
	case got := <-good.inboundMsgChan:
		chainID, err := message.GetChainID(got.Message())
		require.NoError(err)
		require.Equal(cChainID, chainID,
			"a D-Chain message from a peer on a different D-Chain reached the router")
	case <-time.After(5 * time.Second):
		t.Fatal("the C-Chain message was dropped too; exclusion is not per chain")
	}

	select {
	case got := <-good.inboundMsgChan:
		t.Fatalf("a second message arrived: %s", got.Op())
	case <-time.After(100 * time.Millisecond):
	}
}

// TestChainIdentity_RulesDifferenceIsReportedNotEnforced. Same chain, different
// scheduled rules: compatible now, and worth knowing about before the rules
// apply rather than at the moment they do. Never grounds for exclusion — the
// claim is about the peer's binary and cannot be verified from here.
func TestChainIdentity_RulesDifferenceIsReportedNotEnforced(t *testing.T) {
	require := require.New(t)

	mine := dChainG
	mine.Rules = RulesDigest([]byte(`{"activation":100}`))
	theirs := dChainG
	theirs.Rules = RulesDigest([]byte(`{"activation":200}`))
	require.NotEqual(mine.Rules, theirs.Rules)

	viewOfThem, viewOfMe := connect(t, newNode(t, mine), newNode(t, theirs))
	require.Equal(ChainCompatible, viewOfThem.ChainState(dChainID),
		"a different rule generation must not exclude a peer from a chain they agree on")
	require.Equal(ChainCompatible, viewOfMe.ChainState(dChainID))
}

// TestRulesDigest_IgnoresFormattingButNotSchedule. The rules digest answers "do
// these schedule the same thing", so reformatting must not move it and changing
// an activation must.
func TestRulesDigest_IgnoresFormattingButNotSchedule(t *testing.T) {
	require := require.New(t)

	base := RulesDigest([]byte(`{"alpha":1,"beta":{"at":1730446786}}`))

	require.Equal(base, RulesDigest([]byte("{\n  \"beta\" : { \"at\": 1730446786 },\n  \"alpha\": 1\n}\n")),
		"key order and whitespace are not consensus and must not move the digest")
	require.NotEqual(base, RulesDigest([]byte(`{"alpha":1,"beta":{"at":1730446787}}`)),
		"a changed activation must move the digest")
	require.Equal([32]byte{}, RulesDigest(nil),
		"a chain that schedules no rules has no rule generation")

	// An activation above 2^53 must survive exactly; passing it through a float
	// would round it and silently hide a scheduled fork.
	big := RulesDigest([]byte(`{"at":9007199254740993}`))
	require.NotEqual(RulesDigest([]byte(`{"at":9007199254740992}`)), big)

	// Bytes that are not JSON are still hashed, and still stably.
	require.Equal(RulesDigest([]byte("not json")), RulesDigest([]byte("not json")))
	require.NotEqual(RulesDigest([]byte("not json")), RulesDigest([]byte("also not json")))
}

// TestGenesisDigest_IsOverRawBytes. The creation records this protects differ in
// whether they end with a newline and spell the em dash as six ASCII characters;
// anything that normalizes them changes the digest, and a normalizing node would
// exclude the fleet it was meant to join.
func TestGenesisDigest_IsOverRawBytes(t *testing.T) {
	require := require.New(t)

	// As the live records spell it: six ASCII characters, not an em dash. The
	// backquotes matter — an interpreted literal would decode the escape here and
	// the two strings below would be the same bytes, which is precisely the
	// confusion this test exists to catch.
	escaped := `{"description":"Decentralized Exchange \u2014 native CLOB"}`
	decoded := "{\"description\":\"Decentralized Exchange \u2014 native CLOB\"}"
	require.NotEqual(escaped, decoded, "the two forms must be different bytes")

	require.NotEqual(GenesisDigest([]byte(escaped)), GenesisDigest([]byte(escaped+"\n")),
		"the trailing newline is part of the record and matters")
	require.NotEqual(GenesisDigest([]byte(escaped)), GenesisDigest([]byte(decoded)),
		"a JSON round trip turns one into the other and must not survive as the same chain")

	require.NotEqual([32]byte{}, GenesisDigest(nil),
		"a chain with no creation record still has a digest; absence is a wire state, not a chain state")
}

// --- wire compatibility, mirroring the ML-DSA cases in ip_pq_wire_test.go ---

func wireHandshake(chains []*zappb.ChainIdentity) *zappb.Handshake {
	return &zappb.Handshake{
		NetworkId:     1337,
		MyTime:        12345,
		IpAddr:        []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4},
		IpPort:        9650,
		IpSigningTime: 12345,
		IpNodeIdSig:   []byte("tls-sig"),
		TrackedNets:   [][]byte{},
		Client:        &zappb.Client{Name: "lux", Major: 1, Minor: 36, Patch: 58},
		SupportedLps:  []uint32{},
		ObjectedLps:   []uint32{},
		KnownPeers:    &zappb.BloomFilter{Filter: []byte{0x01}, Salt: []byte{0x02}},
		IpBlsSig:      []byte("bls-sig"),
		IpMldsaSig:    []byte("mldsa-sig"),
		Chains:        chains,
	}
}

func marshalHandshakeForTest(t *testing.T, hs *zappb.Handshake) []byte {
	t.Helper()
	raw, err := zappb.Marshal(&zappb.Message{Message: &zappb.Message_Handshake{Handshake: hs}})
	require.NoError(t, err)
	return raw
}

// TestHandshakeWire_LegacyFrameOmitsChains: a peer that sets no chains produces
// a frame the new decoder reads with an empty list, not an error.
func TestHandshakeWire_LegacyFrameOmitsChains(t *testing.T) {
	require := require.New(t)

	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(marshalHandshakeForTest(t, wireHandshake(nil)), &decoded))
	require.Empty(decoded.GetHandshake().Chains)
	require.Equal([]byte("mldsa-sig"), decoded.GetHandshake().IpMldsaSig,
		"the field ahead of the new one must be unaffected")
}

// TestHandshakeWire_TruncatedLegacyFrameDecodesChains: a frame emitted before the
// field existed ends after IpMldsaSig. It must still decode.
func TestHandshakeWire_TruncatedLegacyFrameDecodesChains(t *testing.T) {
	require := require.New(t)

	full := marshalHandshakeForTest(t, wireHandshake(nil))
	legacy := full[:len(full)-4] // strip the zero count a new encoder appends

	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(legacy, &decoded),
		"a pre-upgrade handshake frame must still decode")
	require.Empty(decoded.GetHandshake().Chains)
	require.Equal([]byte("bls-sig"), decoded.GetHandshake().IpBlsSig)
}

// TestHandshakeWire_NewFrameRoundTripsChains: a stated chain survives the wire
// intact, including an absent rule generation staying absent.
func TestHandshakeWire_NewFrameRoundTripsChains(t *testing.T) {
	require := require.New(t)

	sent := dChainG.wire()
	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(
		marshalHandshakeForTest(t, wireHandshake([]*zappb.ChainIdentity{sent})), &decoded))

	require.Len(decoded.GetHandshake().Chains, 1)
	back, err := parseChainIdentity(decoded.GetHandshake().Chains[0])
	require.NoError(err)
	require.Equal(dChainG, back)
	require.Empty(decoded.GetHandshake().Chains[0].RulesId,
		"a chain scheduling no rules states none rather than 32 zero bytes")
}

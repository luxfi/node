// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/formatting"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/version"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"

	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// genesisBytesForValidator builds real P-chain genesis bytes declaring [nodeID]
// as a validator whose signer proves possession of [sk] — the shape the node is
// actually started with, parsed by the same genesis.Parse NewNetwork calls.
func genesisBytesForValidator(t *testing.T, nodeID ids.NodeID, sk bls.Signer) []byte {
	t.Helper()

	return genesisBytesFor(t, declaration{nodeID, popFor(t, sk)})
}

// runGenesisSeededPeer starts two real networks and lets them handshake.
//
// Node 0 is the observer: its validator set is seeded ONLY by NewNetwork from
// [observerGenesis], so whatever the genesis-seeding path computes is the key
// its peer.shouldDisconnect checks node 1's signed IP against. Node 1 is an
// ordinary peer signing its IP with its own BLS key. Reports whether node 0
// accepted node 1.
func runGenesisSeededPeer(t *testing.T, observerGenesis func(peerNodeID ids.NodeID, peerKey bls.Signer) []byte) bool {
	t.Helper()
	require := require.New(t)

	dialer, listeners, nodeIDs, configs := newTestNetwork(t, 2)

	connected := make(chan struct{})
	var closeOnce bool

	networks := make([]Network, len(configs))
	for i, config := range configs {
		config.Beacons = validators.NewManager()
		config.Validators = validators.NewManager()
		config.TrackedChains = set.NewSet[ids.ID](0)
		config.UptimeCalculator = &uptime.NoOpCalculator{}

		handler := &testHandler{}
		if i == 0 {
			// The observer. Its whole validator set comes from genesis seeding.
			config.GenesisBytes = observerGenesis(nodeIDs[1], configs[1].BLSKey)
			handler.ConnectedF = func(nodeID ids.NodeID, _ *version.Application, _ ids.ID) {
				if nodeID == nodeIDs[1] && !closeOnce {
					closeOnce = true
					close(connected)
				}
			}
		}

		net, err := NewNetwork(
			config,
			upgrade.InitiallyActiveTime,
			newMessageCreator(t),
			metric.NewNoOpRegistry(),
			log.NewNoOpLogger(),
			listeners[i],
			dialer,
			handler,
		)
		require.NoError(err)
		networks[i] = net
	}

	// The seeding ran inside NewNetwork. Confirm the observer is actually
	// checking node 1 — a validator entry with no key is never checked at all,
	// and this test would then be asserting nothing.
	vdr, ok := networks[0].(*network).config.Validators.GetValidator(constants.PrimaryNetworkID, nodeIDs[1])
	require.True(ok, "genesis seeding did not register the peer as a validator")
	require.NotEmpty(vdr.PublicKey,
		"genesis seeding registered the peer with NO key, so its signed IP is never verified")

	eg := &errgroup.Group{}
	for i, net := range networks {
		if i == 0 {
			net.ManuallyTrack(configs[1].MyNodeID, endpoints.NewIPEndpoint(configs[1].MyIPPort.Get()))
		}
		eg.Go(net.Dispatch)
	}
	defer func() {
		for _, net := range networks {
			net.StartClose()
		}
		require.NoError(eg.Wait())
	}()

	select {
	case <-connected:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// TestGenesisValidatorKeyIsUsableByPeerVerification drives the seeded key
// through the consumer that reads it.
//
// NewNetwork registers the genesis validators into the node's validators.Manager;
// peer.shouldDisconnect reads that entry to check a peer's signed IP against its
// BLS key. The two halves have to agree on the key, and both of the seeding
// paths produced one shouldDisconnect could not use — so for the whole bootstrap
// window genesis validators went either unverified or dropped outright.
//
// Both cases below are load-bearing, and they fail for opposite reasons:
//
//   - Registering the COMPRESSED form (48 bytes) where peer.go parses
//     uncompressed makes the key unreadable, and the honest peer is dropped.
//   - Registering NO key at all makes shouldDisconnect short-circuit before it
//     verifies anything, and the forged peer is admitted.
func TestGenesisValidatorKeyIsUsableByPeerVerification(t *testing.T) {
	t.Run("honest peer is not dropped", func(t *testing.T) {
		// Genesis names the peer's real key, and the peer signs its IP with it.
		accepted := runGenesisSeededPeer(t, func(peerNodeID ids.NodeID, peerKey bls.Signer) []byte {
			return genesisBytesForValidator(t, peerNodeID, peerKey)
		})
		require.True(t, accepted,
			"a genesis validator signing its IP with the key genesis declares was DROPPED")
	})

	t.Run("forged peer is dropped", func(t *testing.T) {
		// Genesis names a different key than the peer holds, so the peer's
		// signed IP is a forgery for the validator it claims to be.
		accepted := runGenesisSeededPeer(t, func(peerNodeID ids.NodeID, _ bls.Signer) []byte {
			attacker, err := localsigner.New()
			require.NoError(t, err)
			return genesisBytesForValidator(t, peerNodeID, attacker)
		})
		require.False(t, accepted,
			"a peer whose signed IP does not match the key genesis declares was ADMITTED")
	})
}

// TestCanonicalGenesisConfigKeyIsUsableByPeerVerification covers the other
// seeding path — the canonical config NewNetwork falls back to when the node is
// started without genesis bytes.
//
// That config carries the key as a 0x-prefixed hex string of the COMPRESSED
// key, the same field genesis/builder decodes with formatting.HexNC. Base64
// does not fail cleanly on hex — every character is in its alphabet — so
// decoding it that way returned a short read of garbage plus an error that was
// discarded. Garbage of non-zero length passes shouldDisconnect's has-a-key
// guard and then fails its uncompressed reader, which drops precisely the
// validators the node needs in order to sync.
//
// These stakers have fixed node IDs, so no peer in a test can hold their keys
// and complete a handshake. What is checkable is the thing shouldDisconnect
// actually does with the entry: parse it with
// bls.PublicKeyFromValidUncompressedBytes, and get back the declared key.
func TestCanonicalGenesisConfigKeyIsUsableByPeerVerification(t *testing.T) {
	require := require.New(t)

	genesisConfig := builder.GetConfig(constants.LocalID)
	require.NotNil(genesisConfig)

	declared := map[ids.NodeID]string{}
	for _, staker := range genesisConfig.InitialStakers {
		if staker.Signer != nil && staker.Signer.PublicKey != "" {
			declared[staker.NodeID] = staker.Signer.PublicKey
		}
	}
	require.NotEmpty(declared, "canonical config carries no keyed stakers, so this test would assert nothing")

	_, listeners, _, configs := newTestNetwork(t, 1)
	config := configs[0]
	config.Beacons = validators.NewManager()
	config.Validators = validators.NewManager()
	config.TrackedChains = set.NewSet[ids.ID](0)
	config.UptimeCalculator = &uptime.NoOpCalculator{}
	// No genesis bytes, so NewNetwork falls back to the canonical config.
	config.GenesisBytes = nil
	config.NetworkID = constants.LocalID

	net, err := NewNetwork(
		config,
		upgrade.InitiallyActiveTime,
		newMessageCreator(t),
		metric.NewNoOpRegistry(),
		log.NewNoOpLogger(),
		listeners[0],
		newTestDialer(),
		&testHandler{},
	)
	require.NoError(err)
	defer net.StartClose()

	for nodeID, publicKey := range declared {
		vdr, ok := config.Validators.GetValidator(constants.PrimaryNetworkID, nodeID)
		require.True(ok, "canonical genesis staker %s was not seeded", nodeID)

		// The reader peer.shouldDisconnect uses. A key it cannot parse makes the
		// peer disconnect on sight.
		got := bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey)
		require.NotNil(got,
			"canonical genesis staker %s was seeded with a key shouldDisconnect cannot parse, so it is dropped on sight", nodeID)

		// And it is the key the config declares, not some other bytes that
		// happen to parse.
		want, err := formatting.Decode(formatting.HexNC, publicKey)
		require.NoError(err)
		require.Equal(want, bls.PublicKeyToCompressedBytes(got),
			"canonical genesis staker %s was seeded with a DIFFERENT key than the config declares", nodeID)
	}
}

// declaration is a genesis entry under test: a node ID and the signer genesis
// declares for it.
type declaration struct {
	nodeID ids.NodeID
	sig    signer.Signer
}

// popFor is [sk]'s proof that it holds its own key.
func popFor(t *testing.T, sk bls.Signer) *signer.ProofOfPossession {
	t.Helper()

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	return pop
}

// honestPoP is a fresh key together with its own possession proof.
func honestPoP(t *testing.T) *signer.ProofOfPossession {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)
	return popFor(t, sk)
}

// forgedPoP is a well-formed key carrying somebody else's possession proof.
//
// Both halves are valid curve points, so it survives the wire round trip and
// its key parses. Only the pairing rejects it — which is exactly the case
// tx.PublicKey() reports as an error, and the only forgery worth testing: one
// whose key did not parse would prove nothing about the seeding decision.
func forgedPoP(t *testing.T) *signer.ProofOfPossession {
	t.Helper()
	require := require.New(t)

	forged := *honestPoP(t)
	forged.ProofOfPossession = honestPoP(t).ProofOfPossession

	parsed, err := bls.PublicKeyFromCompressedBytes(forged.PublicKey[:])
	require.NoError(err, "the forged signer's key must parse — otherwise this proves nothing")
	require.NotNil(parsed)
	require.Error(forged.Verify(), "a forged possession proof must not verify")

	return &forged
}

// genesisBytesFor builds real P-chain genesis declaring one validator per
// entry, using the same encoder that writes the genesis a node is started with.
func genesisBytesFor(t *testing.T, vdrs ...declaration) []byte {
	t.Helper()
	require := require.New(t)

	txsIn := make([]*txs.Tx, 0, len(vdrs))
	for _, v := range vdrs {
		utx, err := txs.NewAddPermissionlessValidatorTx(
			&lux.BaseTx{},
			txs.Validator{NodeID: v.nodeID, Wght: 100},
			constants.PrimaryNetworkID,
			v.sig,
			nil,
			&secp256k1fx.OutputOwners{},
			&secp256k1fx.OutputOwners{},
			0,
		)
		require.NoError(err)

		tx := &txs.Tx{Unsigned: utx}
		require.NoError(tx.Initialize())
		txsIn = append(txsIn, tx)
	}

	genesisBytes, err := (&genesis.Genesis{Validators: txsIn}).Bytes()
	require.NoError(err)

	// genesis.Parse is a wire decode and checks no possession proof, so a
	// forged validator does reach the seeding path. If that ever changes these
	// tests stop covering the seeding decision and say so here.
	parsedGenesis, err := genesis.Parse(genesisBytes)
	require.NoError(err)
	require.Len(parsedGenesis.Validators, len(vdrs))

	return genesisBytes
}

// seedFromGenesis runs NewNetwork's genesis-seeding path over [genesisBytes]
// for [networkID] and returns the validator set it produced.
func seedFromGenesis(t *testing.T, networkID uint32, genesisBytes []byte) validators.Manager {
	t.Helper()
	require := require.New(t)

	_, listeners, _, configs := newTestNetwork(t, 1)
	config := configs[0]
	config.Beacons = validators.NewManager()
	config.Validators = validators.NewManager()
	config.TrackedChains = set.NewSet[ids.ID](0)
	config.UptimeCalculator = &uptime.NoOpCalculator{}
	config.NetworkID = networkID
	config.GenesisBytes = genesisBytes

	net, err := NewNetwork(
		config,
		upgrade.InitiallyActiveTime,
		newMessageCreator(t),
		metric.NewNoOpRegistry(),
		log.NewNoOpLogger(),
		listeners[0],
		newTestDialer(),
		&testHandler{},
	)
	require.NoError(err)
	t.Cleanup(net.StartClose)

	return config.Validators
}

// TestGenesisValidatorWithUnverifiableSignerIsNotSeeded holds the seeding path
// to failing CLOSED, and to failing closed only where it should.
//
// A signer that does not verify is a declared key nothing can be checked
// against. Seeding it anyway kept the validator's full genesis weight while
// peer.shouldDisconnect short-circuits on a keyless entry
// (vdr.PublicKey == nil ⇒ return false), so the entry would vote for the whole
// bootstrap window with nobody proving they hold the key — the same fail-open
// the surrounding seeding fixes closed elsewhere.
//
// The other two validators are what keep the skip surgical: an honest one says
// the seeding path ran at all, and one that declares no key at all is seeded
// keyless, because keyless is what it says it is and the wire allows it.
func TestGenesisValidatorWithUnverifiableSignerIsNotSeeded(t *testing.T) {
	require := require.New(t)

	honest, keyless, forged := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	vdrs := seedFromGenesis(t, constants.LocalID, genesisBytesFor(t,
		declaration{honest, honestPoP(t)},
		declaration{keyless, &signer.Empty{}},
		declaration{forged, forgedPoP(t)},
	))

	honestVdr, ok := vdrs.GetValidator(constants.PrimaryNetworkID, honest)
	require.True(ok, "the honest genesis validator was not seeded, so this test asserts nothing")
	require.NotEmpty(honestVdr.PublicKey)

	keylessVdr, ok := vdrs.GetValidator(constants.PrimaryNetworkID, keyless)
	require.True(ok, "a validator declaring no key is a shape the wire allows and must still be seeded")
	require.Empty(keylessVdr.PublicKey)

	// The whole point. Not "seeded with no key" — not seeded.
	if forgedVdr, ok := vdrs.GetValidator(constants.PrimaryNetworkID, forged); ok {
		require.Fail("genesis validator seeded despite an unverifiable signer",
			"weight %d, key %x — and a keyless entry is never checked by peer.shouldDisconnect",
			forgedVdr.Weight, forgedVdr.PublicKey)
	}
}

// TestEmptiedGenesisSetIsNotReplacedByTheCanonicalOne holds the canonical
// fallback to the one question it answers.
//
// The fallback used to be gated on the seeded slice being empty, which was
// unreachable while an unverifiable validator was still seeded keyless. Failing
// closed makes it reachable: a node started with custom genesis whose every
// validator is skipped now empties that slice, and the fallback would hand it
// builder.GetConfig(NetworkID) — a DIFFERENT network's stakers, answering "who
// validates here?" with nodes that do not.
//
// A genesis that declared a set and lost all of it leaves this node with no
// validators. That is the answer; it bootstraps from its beacons.
func TestEmptiedGenesisSetIsNotReplacedByTheCanonicalOne(t *testing.T) {
	require := require.New(t)

	// A network whose canonical config carries stakers, so a substitution is
	// visible here rather than indistinguishable from the right answer.
	canonical := builder.GetConfig(constants.LocalID)
	require.NotNil(canonical)
	require.NotEmpty(canonical.InitialStakers,
		"the canonical config declares no stakers, so a wrong fallback would look exactly like the right answer")

	vdrs := seedFromGenesis(t, constants.LocalID, genesisBytesFor(t,
		declaration{ids.GenerateTestNodeID(), forgedPoP(t)},
		declaration{ids.GenerateTestNodeID(), forgedPoP(t)},
	))

	require.Empty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID),
		"a genesis whose every validator was skipped seeded validators anyway; if these are the "+
			"canonical config's node IDs, the fallback substituted another network's set")
}

// TestDeclaredKeyRefusesAKeyThatIsNotOne holds the canonical half of the
// seeding decision to failing CLOSED.
//
// builder.GetConfig reads the shipped configs and every one of them decodes, so
// the canonical loop cannot be shown a bad staker through it — and there is no
// seam to inject one, by design. declaredKey IS that decision, so this checks
// the decision itself rather than a way of reaching it.
func TestDeclaredKeyRefusesAKeyThatIsNotOne(t *testing.T) {
	declaredPoP := honestPoP(t)

	real, err := formatting.Encode(formatting.HexNC, declaredPoP.PublicKey[:])
	require.NoError(t, err)
	realKey, err := bls.PublicKeyFromCompressedBytes(declaredPoP.PublicKey[:])
	require.NoError(t, err)

	// Right length, valid hex, not a point on the curve.
	notAPoint, err := formatting.Encode(formatting.HexNC, bytes.Repeat([]byte{0xff}, bls.PublicKeyLen))
	require.NoError(t, err)

	withKey := func(key string) genesiscfg.Staker {
		return genesiscfg.Staker{Signer: &genesiscfg.ProofOfPossession{PublicKey: key}}
	}

	for _, test := range []struct {
		name   string
		staker genesiscfg.Staker
		want   []byte
		refuse bool
	}{
		{
			// Declaring no key is a shape the config allows, and such a staker
			// is seeded keyless because keyless is what it says it is.
			name:   "no signer at all",
			staker: genesiscfg.Staker{},
		},
		{
			name:   "signer declaring no key",
			staker: withKey(""),
		},
		{
			name:   "key that is not hex",
			staker: withKey("0xnot-a-key"),
			refuse: true,
		},
		{
			// The case base64 used to turn into 72 bytes of garbage that then
			// passed the has-a-key guard and failed the uncompressed reader.
			name:   "hex that is not a curve point",
			staker: withKey(notAPoint),
			refuse: true,
		},
		{
			name:   "a real key",
			staker: withKey(real),
			want:   bls.PublicKeyToUncompressedBytes(realKey),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			key, err := declaredKey(test.staker)
			if test.refuse {
				require.Error(err, "a declared key that is not a key was accepted")
				require.Nil(key, "a refused staker must carry no key, keyless least of all")
				return
			}
			require.NoError(err)
			require.Equal(test.want, key)
		})
	}
}

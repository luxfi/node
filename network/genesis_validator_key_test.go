// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/formatting"
	genesisconfigs "github.com/luxfi/genesis/configs"
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
// for [networkID], through [logger], and returns the validator set it produced.
func seedFromGenesis(t *testing.T, networkID uint32, genesisBytes []byte, logger log.Logger) validators.Manager {
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
		logger,
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
	), log.NewNoOpLogger())

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
	), log.NewNoOpLogger())

	require.Empty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID),
		"a genesis whose every validator was skipped seeded validators anyway; if these are the "+
			"canonical config's node IDs, the fallback substituted another network's set")
}

// declaredBy is [pop] the way a canonical config declares it: both halves as
// 0x-prefixed hex, of the COMPRESSED key and of the proof over it.
func declaredBy(t *testing.T, pop *signer.ProofOfPossession) genesiscfg.Staker {
	t.Helper()
	require := require.New(t)

	pk, err := formatting.Encode(formatting.HexNC, pop.PublicKey[:])
	require.NoError(err)
	proof, err := formatting.Encode(formatting.HexNC, pop.ProofOfPossession[:])
	require.NoError(err)

	return genesiscfg.Staker{
		NodeID: ids.GenerateTestNodeID(),
		Signer: &genesiscfg.ProofOfPossession{PublicKey: pk, ProofOfPossession: proof},
	}
}

// TestDeclaredKeyRefusesAKeyThatIsNotOne holds the canonical half of the
// seeding decision to failing CLOSED.
//
// builder.GetConfig reads the shipped configs and every one of them verifies,
// so a bad staker does not arrive through the shipped ones. It can arrive
// through the overrides that feed the same call — PCHAIN_ALLOCS, its file
// sibling, and the genesis trees under $HOME — which replace the staker list
// outright. declaredKey IS the decision every one of them lands on, so this
// checks the decision itself rather than a way of reaching it.
//
// A key that PARSES is only half of it. genesis/builder verifies the possession
// proof when it builds genesis from this same config, so a staker it refuses
// and this path seeds would be one field read two ways. The forged case below
// is that difference: both halves are valid curve points, so only the pairing
// tells them apart.
func TestDeclaredKeyRefusesAKeyThatIsNotOne(t *testing.T) {
	honest := honestPoP(t)
	realKey, err := bls.PublicKeyFromCompressedBytes(honest.PublicKey[:])
	require.NoError(t, err)

	realHex, err := formatting.Encode(formatting.HexNC, honest.PublicKey[:])
	require.NoError(t, err)
	realProof, err := formatting.Encode(formatting.HexNC, honest.ProofOfPossession[:])
	require.NoError(t, err)

	// Right length, valid hex, not a point on the curve.
	notAPoint, err := formatting.Encode(formatting.HexNC, bytes.Repeat([]byte{0xff}, bls.PublicKeyLen))
	require.NoError(t, err)

	withKey := func(key string) genesiscfg.Staker {
		return genesiscfg.Staker{Signer: &genesiscfg.ProofOfPossession{PublicKey: key}}
	}
	signedBy := func(key, proof string) genesiscfg.Staker {
		return genesiscfg.Staker{Signer: &genesiscfg.ProofOfPossession{PublicKey: key, ProofOfPossession: proof}}
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
			// A key nobody proved they hold. genesis/builder refuses this
			// staker, so seeding it here would read one field two ways.
			name:   "a real key with no proof at all",
			staker: withKey(realHex),
			refuse: true,
		},
		{
			name:   "a real key whose proof is not hex",
			staker: signedBy(realHex, "0xnot-a-proof"),
			refuse: true,
		},
		{
			name:   "a real key whose proof is the right length and not a signature",
			staker: signedBy(realHex, mustHex(t, bytes.Repeat([]byte{0xff}, bls.SignatureLen))),
			refuse: true,
		},
		{
			// The only forgery worth testing: both halves parse, and only the
			// pairing rejects it.
			name:   "a real key carrying somebody else's proof",
			staker: signedBy(realHex, mustHex(t, honestPoP(t).ProofOfPossession[:])),
			refuse: true,
		},
		{
			name:   "a real key and its own proof",
			staker: signedBy(realHex, realProof),
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

// mustHex is [b] as the canonical config writes it.
func mustHex(t *testing.T, b []byte) string {
	t.Helper()

	s, err := formatting.Encode(formatting.HexNC, b)
	require.NoError(t, err)
	return s
}

// TestShippedConfigsStillSeedTheirFullValidatorSet is the cost of checking the
// possession proof here: a shipped config whose proof does not verify loses that
// validator, on every node that falls back to it.
//
// So it is checked, over every network builder.GetConfig serves — the shipped
// stakers ARE the population this path runs against in production, and the
// question "does the stricter rule drop anybody?" has one answer per staker.
func TestShippedConfigsStillSeedTheirFullValidatorSet(t *testing.T) {
	// SHIPPED is the whole claim, so nothing a box happens to have configured
	// may stand in for one.
	unconfigured(t)

	// The well-known networks and the chain-ID aliases GetConfig also answers
	// to, named by the package that SHIPS them — luxfi/constants carries its own
	// copy of the aliases and they do not all agree (its DevnetChainID is 96370,
	// which no shipped config answers to).
	stakers := 0
	for _, networkID := range []uint32{
		genesisconfigs.MainnetID, genesisconfigs.TestnetID, genesisconfigs.DevnetID, genesisconfigs.LocalID,
		genesisconfigs.MainnetChainID, genesisconfigs.TestnetChainID, genesisconfigs.DevnetChainID, genesisconfigs.LocalChainID,
	} {
		t.Run(fmt.Sprint(networkID), func(t *testing.T) {
			require := require.New(t)

			cfg := builder.GetConfig(networkID)
			require.NotNil(cfg)
			require.NotEmpty(cfg.InitialStakers,
				"network %d ships no stakers, so this case asserts nothing", networkID)

			seeded, refused := canonicalStakers(cfg.InitialStakers)
			require.Empty(refused, "the shipped config for network %d loses validators to the key check", networkID)
			require.Len(seeded, len(cfg.InitialStakers))

			for i, s := range seeded {
				require.NotEmpty(s.BLSKey,
					"shipped staker %s declares a key and was seeded keyless", cfg.InitialStakers[i].NodeID)
				require.NotNil(bls.PublicKeyFromValidUncompressedBytes(s.BLSKey),
					"shipped staker %s was seeded with a key peer.shouldDisconnect cannot parse", s.NodeID)
			}
			stakers += len(seeded)
		})
	}
	require.NotZero(t, stakers, "no shipped config was read, so this test asserts nothing")
}

// TestCanonicalStakersRefusesRatherThanSeedingKeyless closes the last place the
// seeding decision could fail open.
//
// The canonical path used to skip a bad staker with a `continue` inside
// NewNetwork, reachable only through builder.GetConfig — and every shipped
// config verifies, so nothing could drive it. As a mapping it is a value: a
// refused staker is ABSENT from the seeded set, and seeding it keyless instead
// (full genesis weight, and peer.shouldDisconnect returning early on it) fails
// right here.
func TestCanonicalStakersRefusesRatherThanSeedingKeyless(t *testing.T) {
	require := require.New(t)

	honest := declaredBy(t, honestPoP(t))
	forged := declaredBy(t, forgedPoP(t))

	garbage := declaredBy(t, honestPoP(t))
	garbage.Signer.PublicKey = "0xnot-a-key"

	keyless := genesiscfg.Staker{NodeID: ids.GenerateTestNodeID()}

	// Weight is the other half of what seeding a refused staker would hand it.
	weighty := honest
	weighty.NodeID = ids.GenerateTestNodeID()
	weighty.Weight = 42

	seeded, refused := canonicalStakers([]genesiscfg.Staker{honest, forged, garbage, keyless, weighty})

	got := map[ids.NodeID]genesisStaker{}
	for _, s := range seeded {
		got[s.NodeID] = s
	}
	require.Len(got, 3, "the seeded set is not exactly the stakers that proved their keys")

	require.Contains(got, honest.NodeID)
	require.NotEmpty(got[honest.NodeID].BLSKey)
	require.EqualValues(1, got[honest.NodeID].Weight, "a staker declaring no weight must still count for one")

	require.Contains(got, keyless.NodeID, "a staker declaring no key is a shape the config allows")
	require.Empty(got[keyless.NodeID].BLSKey)

	require.Contains(got, weighty.NodeID)
	require.EqualValues(42, got[weighty.NodeID].Weight)

	// The whole point. Not "seeded with no key" — not seeded.
	require.NotContains(got, forged.NodeID,
		"a staker whose declared key it never proved it holds was seeded")
	require.NotContains(got, garbage.NodeID,
		"a staker whose declared key is not a key was seeded")

	refusedIDs := make([]ids.NodeID, 0, len(refused))
	for _, r := range refused {
		require.Error(r.Err)
		refusedIDs = append(refusedIDs, r.NodeID)
	}
	require.ElementsMatch([]ids.NodeID{forged.NodeID, garbage.NodeID}, refusedIDs,
		"the refusals must name exactly the stakers that were not seeded, so the node can say which")
}

// recorder keeps what is written through it at Error and at Warn, and is
// otherwise the no-op logger.
//
// The genesis seeding runs inside NewNetwork, before anything is dispatched, so
// what it wrote is readable the moment NewNetwork returns. The lock is for the
// network's own goroutines, not for that.
type recorder struct {
	log.Logger

	lock  sync.Mutex
	errs  []string
	warns []string
}

func newRecorder() *recorder {
	return &recorder{Logger: log.NewNoOpLogger()}
}

func (l *recorder) Error(msg string, _ ...interface{}) {
	l.lock.Lock()
	defer l.lock.Unlock()

	l.errs = append(l.errs, msg)
}

func (l *recorder) Warn(msg string, _ ...interface{}) {
	l.lock.Lock()
	defer l.lock.Unlock()

	l.warns = append(l.warns, msg)
}

func (l *recorder) errors() []string {
	l.lock.Lock()
	defer l.lock.Unlock()

	return slices.Clone(l.errs)
}

func (l *recorder) warnings() []string {
	l.lock.Lock()
	defer l.lock.Unlock()

	return slices.Clone(l.warns)
}

// TestEmptyValidatorSetIsAnError holds the node to SAYING it has no validators.
//
// The error used to be keyed on the why — a genesis that declared stakers and
// lost them all — so the other way to get there said nothing at all: a node
// whose canonical config declares no validator started empty in silence, which
// is the shape a misconfigured network ID has. Keying it on the END STATE makes
// both audible, and the reason still separates them.
func TestEmptyValidatorSetIsAnError(t *testing.T) {
	const unknownNetwork = 424242

	t.Run("the canonical config declares nobody", func(t *testing.T) {
		require := require.New(t)

		// An unknown network ID resolves to the localnet NAME, and that name is
		// looked up under $HOME before it is given up on. On a box with a
		// genesis checkout this case reads a real validator set off the
		// developer's disk and stops being the case it is named for.
		unconfigured(t)

		require.Empty(builder.GetConfig(unknownNetwork).InitialStakers,
			"network %d ships stakers after all, so this case proves nothing", unknownNetwork)

		logger := newRecorder()
		vdrs := seedFromGenesis(t, unknownNetwork, nil, logger)

		require.Empty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID))
		require.Contains(logger.errors(), emptyValidatorSetReason(0, 0),
			"a node started with no validators at all said nothing about it")
	})

	t.Run("this node's genesis lost every validator it declared", func(t *testing.T) {
		require := require.New(t)

		logger := newRecorder()
		vdrs := seedFromGenesis(t, constants.LocalID, genesisBytesFor(t,
			declaration{ids.GenerateTestNodeID(), forgedPoP(t)},
		), logger)

		require.Empty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID))
		require.Contains(logger.errors(), emptyValidatorSetReason(1, 0),
			"a node that refused every validator its genesis declared said nothing about it")
	})

	t.Run("the canonical config lost every validator it declares", func(t *testing.T) {
		require := require.New(t)

		// This node declares nothing, so it falls back — and what it falls
		// back to declares a validator that cannot prove its key. The canonical
		// config DID name somebody, so saying nobody was named would name the
		// wrong why for the one path that reaches it.
		declareCanonically(t, forgedPoP(t))

		logger := newRecorder()
		vdrs := seedFromGenesis(t, constants.LocalID, nil, logger)

		require.Empty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID))
		require.Contains(logger.errors(), emptyValidatorSetReason(0, 1),
			"a node that refused every validator the canonical config declares said it was declared none")
	})

	t.Run("a seeded node says none of them", func(t *testing.T) {
		require := require.New(t)

		logger := newRecorder()
		vdrs := seedFromGenesis(t, constants.LocalID, genesisBytesFor(t,
			declaration{ids.GenerateTestNodeID(), honestPoP(t)},
		), logger)

		require.NotEmpty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID))
		for _, reason := range emptyValidatorSetReasons() {
			require.NotContains(logger.errors(), reason)
		}
	})

	t.Run("the reason separates all three", func(t *testing.T) {
		// A set of them is as large as the slice only if no two read alike.
		reasons := emptyValidatorSetReasons()
		require.Len(t, reasons, 3)
		require.Equal(t, len(reasons), set.Of(reasons...).Len(),
			"two ways to an empty validator set read the same, so the log cannot tell them apart")
	})
}

// emptyValidatorSetReasons is every way the node can say it has no validators,
// in the order emptyValidatorSetReason answers them.
func emptyValidatorSetReasons() []string {
	return []string{
		emptyValidatorSetReason(1, 0),
		emptyValidatorSetReason(0, 1),
		emptyValidatorSetReason(0, 0),
	}
}

// unconfigured cuts every override that feeds builder.GetConfig, for the
// duration of the test.
//
// Two environment variables and two home-directory paths each replace a
// network's stakers before any caller sees them. A test that asks what a
// network ships — or what an unknown one does NOT ship — otherwise answers with
// whatever the box it runs on happens to have configured, and passes or fails
// for a reason that is not in the test.
func unconfigured(t *testing.T) {
	t.Helper()

	t.Setenv("PCHAIN_ALLOCS", "")
	t.Setenv("PCHAIN_ALLOCS_FILE", "")
	t.Setenv("HOME", t.TempDir())
}

// declareCanonically makes the canonical config for LocalID declare exactly one
// staker carrying [pop], for the duration of the test.
//
// PCHAIN_ALLOCS is read by the same builder.GetConfig the fallback calls, and a
// P-chain shard supplied through it replaces the embedded initialStakers
// wholesale — the seam by which a canonical config comes to name a node this
// network never chose. Allocations must be non-empty for the shard to be taken
// at all, and the stake duration non-zero or the embedded stakers are merged
// back in; both are copied from the localnet config so only the staker list
// differs.
func declareCanonically(t *testing.T, pop *signer.ProofOfPossession) {
	t.Helper()
	require := require.New(t)

	const localnetRewardAddr = "P-local1tuhk0usyez9w520ftjw7mdctkky4yrheyx62w9"

	shard, err := json.Marshal(map[string]any{
		"initialStakeDuration": 31536000,
		"allocations": []any{map[string]any{
			"initialAmount": 50000000000000,
			"utxoAddr":      localnetRewardAddr,
			"evmAddr":       "0x5369615110ca435bdf798f31c20ba6163d7b0a54",
		}},
		"initialStakedFunds": []string{localnetRewardAddr},
		"initialStakers": []any{map[string]any{
			"nodeID":        ids.GenerateTestNodeID().String(),
			"rewardAddress": localnetRewardAddr,
			"delegationFee": 20000,
			"signer": map[string]any{
				"publicKey":         mustHex(t, pop.PublicKey[:]),
				"proofOfPossession": mustHex(t, pop.ProofOfPossession[:]),
			},
		}},
	})
	require.NoError(err)
	t.Setenv("PCHAIN_ALLOCS", string(shard))

	require.Len(builder.GetConfig(constants.LocalID).InitialStakers, 1,
		"the canonical config did not take the declared staker, so this case proves nothing")
}

// TestUnreadableGenesisIsAWarning holds the substitution to being audible.
//
// Bytes that do not parse leave the declared count at zero, which is the same
// state as no genesis at all — so the node takes the CANONICAL set for its
// network ID and validates against stakers its own genesis never named. That
// was a Debug line, i.e. nothing, on a node running with a corrupt genesis.
func TestUnreadableGenesisIsAWarning(t *testing.T) {
	require := require.New(t)

	logger := newRecorder()
	vdrs := seedFromGenesis(t, constants.LocalID, []byte("this is not a P-chain genesis"), logger)

	require.Contains(logger.warnings(), "failed to parse P-chain genesis bytes, will try canonical config",
		"a node whose genesis could not be read said nothing about it")

	// And this is what that silence was hiding: the canonical set, seeded under
	// a genesis the node could not read.
	require.NotEmpty(vdrs.GetValidatorIDs(constants.PrimaryNetworkID))
}

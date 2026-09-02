// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/formatting"
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
	require := require.New(t)

	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	utx, err := txs.NewAddPermissionlessValidatorTx(
		&lux.BaseTx{},
		txs.Validator{NodeID: nodeID, Wght: 100},
		constants.PrimaryNetworkID,
		pop,
		nil,
		&secp256k1fx.OutputOwners{},
		&secp256k1fx.OutputOwners{},
		0,
	)
	require.NoError(err)

	tx := &txs.Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	genesisBytes, err := (&genesis.Genesis{Validators: []*txs.Tx{tx}}).Bytes()
	require.NoError(err)

	// The seeding path this test exercises only runs if genesis.Parse succeeds
	// and yields a validator, so hold it to that here rather than discovering a
	// silently-skipped path as a passing test.
	parsed, err := genesis.Parse(genesisBytes)
	require.NoError(err)
	require.Len(parsed.Validators, 1)

	return genesisBytes
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

// genesisBytesForPair builds real P-chain genesis declaring two validators: one
// whose signer proves possession of its key, and one whose signer carries a
// well-formed key together with somebody else's possession proof.
//
// Both halves of the forged signer are valid curve points, so it survives the
// wire round trip and its key parses. Only the pairing rejects it — which is
// exactly the case tx.PublicKey() reports as an error.
func genesisBytesForPair(t *testing.T, honest, forged ids.NodeID) []byte {
	t.Helper()
	require := require.New(t)

	honestKey, err := localsigner.New()
	require.NoError(err)
	honestPoP, err := signer.NewProofOfPossession(honestKey)
	require.NoError(err)

	victim, err := localsigner.New()
	require.NoError(err)
	victimPoP, err := signer.NewProofOfPossession(victim)
	require.NoError(err)
	other, err := localsigner.New()
	require.NoError(err)
	otherPoP, err := signer.NewProofOfPossession(other)
	require.NoError(err)

	forgedPoP := *victimPoP
	forgedPoP.ProofOfPossession = otherPoP.ProofOfPossession

	// The forgery is the interesting kind: the key parses, possession does not
	// hold. Without both of these the test would be proving something else.
	parsed, err := bls.PublicKeyFromCompressedBytes(forgedPoP.PublicKey[:])
	require.NoError(err, "the forged signer's key must parse — otherwise this proves nothing")
	require.NotNil(parsed)
	require.Error(forgedPoP.Verify(), "a forged possession proof must not verify")

	txsIn := make([]*txs.Tx, 0, 2)
	for _, v := range []struct {
		nodeID ids.NodeID
		pop    *signer.ProofOfPossession
	}{
		{honest, honestPoP},
		{forged, &forgedPoP},
	} {
		utx, err := txs.NewAddPermissionlessValidatorTx(
			&lux.BaseTx{},
			txs.Validator{NodeID: v.nodeID, Wght: 100},
			constants.PrimaryNetworkID,
			v.pop,
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

	// genesis.Parse is a wire decode and checks no possession proof, so the
	// forged validator does reach the seeding path. If that ever changes this
	// test stops covering the seeding decision and says so here.
	parsedGenesis, err := genesis.Parse(genesisBytes)
	require.NoError(err)
	require.Len(parsedGenesis.Validators, 2)

	return genesisBytes
}

// seedFromGenesis runs NewNetwork's genesis-seeding path over [genesisBytes]
// and returns the validator set it produced.
func seedFromGenesis(t *testing.T, genesisBytes []byte) validators.Manager {
	t.Helper()
	require := require.New(t)

	_, listeners, _, configs := newTestNetwork(t, 1)
	config := configs[0]
	config.Beacons = validators.NewManager()
	config.Validators = validators.NewManager()
	config.TrackedChains = set.NewSet[ids.ID](0)
	config.UptimeCalculator = &uptime.NoOpCalculator{}
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
// to failing CLOSED.
//
// A signer that does not verify is a declared key nothing can be checked
// against. Seeding it anyway kept the validator's full genesis weight while
// peer.shouldDisconnect short-circuits on a keyless entry
// (vdr.PublicKey == nil ⇒ return false), so the entry would vote for the whole
// bootstrap window with nobody proving they hold the key — the same fail-open
// the surrounding seeding fixes closed elsewhere.
//
// The honest validator in the same genesis is the control: it says the seeding
// path ran and that the skip is surgical rather than a wholesale bail-out.
func TestGenesisValidatorWithUnverifiableSignerIsNotSeeded(t *testing.T) {
	require := require.New(t)

	honest, forged := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	vdrs := seedFromGenesis(t, genesisBytesForPair(t, honest, forged))

	honestVdr, ok := vdrs.GetValidator(constants.PrimaryNetworkID, honest)
	require.True(ok, "the honest genesis validator was not seeded, so this test asserts nothing")
	require.NotEmpty(honestVdr.PublicKey)

	// The whole point. Not "seeded with no key" — not seeded.
	if forgedVdr, ok := vdrs.GetValidator(constants.PrimaryNetworkID, forged); ok {
		require.Fail("genesis validator seeded despite an unverifiable signer",
			"weight %d, key %x — and a keyless entry is never checked by peer.shouldDisconnect",
			forgedVdr.Weight, forgedVdr.PublicKey)
	}
}

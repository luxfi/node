// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/formatting"

	genesisconfigs "github.com/luxfi/genesis/configs"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"

	"github.com/luxfi/node/vms/platformvm/signer"
)

// hexOf is [b] as a config writes it: 0x-prefixed hex, no checksum.
func hexOf(t *testing.T, b []byte) string {
	t.Helper()

	s, err := formatting.Encode(formatting.HexNC, b)
	require.NoError(t, err)
	return s
}

// honestPoP is a fresh key together with its own possession proof.
func honestPoP(t *testing.T) *signer.ProofOfPossession {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(t, err)
	return pop
}

// TestParseProofOfPossessionRefusesAFieldThatDoesNotFit holds the builder to
// reading the field the operator wrote.
//
// Both halves land in fixed arrays, so a copy takes the first N bytes of a long
// field and zero-pads a short one. The long case is the one that gets away with
// it: a key with bytes appended keeps its valid prefix, so the truncated pair
// verifies and genesis is built from a key nobody wrote — under a node ID that
// did not declare it.
//
// network.declaredKey reads this same pair for the same config and refuses both
// lengths. A staker one accepts and the other refuses is one field read two
// ways, so this is what makes the two agree.
func TestParseProofOfPossessionRefusesAFieldThatDoesNotFit(t *testing.T) {
	honest := honestPoP(t)
	key := honest.PublicKey[:]
	proof := honest.ProofOfPossession[:]

	// A byte appended to a field that verifies. Strip it and what is left is
	// the honest pair, which is the whole difficulty: only the length says so.
	longer := func(b []byte) []byte { return append(bytes.Clone(b), 0x00) }
	shorter := func(b []byte) []byte { return bytes.Clone(b)[:len(b)-1] }

	for _, test := range []struct {
		name   string
		key    []byte
		proof  []byte
		refuse bool
	}{
		{
			name:  "the pair as it was written",
			key:   key,
			proof: proof,
		},
		{
			name:   "a key carrying a trailing byte",
			key:    longer(key),
			proof:  proof,
			refuse: true,
		},
		{
			name:   "a key missing its last byte",
			key:    shorter(key),
			proof:  proof,
			refuse: true,
		},
		{
			name:   "a proof carrying a trailing byte",
			key:    key,
			proof:  longer(proof),
			refuse: true,
		},
		{
			name:   "a proof missing its last byte",
			key:    key,
			proof:  shorter(proof),
			refuse: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			got, err := parseProofOfPossession(&genesiscfg.ProofOfPossession{
				PublicKey:         hexOf(t, test.key),
				ProofOfPossession: hexOf(t, test.proof),
			})
			if test.refuse {
				require.Error(err, "a field that does not fit the array was silently resized to fit it")
				require.Nil(got)
				return
			}
			require.NoError(err)
			require.Equal(honest.PublicKey, got.PublicKey)
			require.Equal(honest.ProofOfPossession, got.ProofOfPossession)
		})
	}

	// Nothing above should have needed the array bounds spelled out, but the
	// guard reads its lengths off the destination, so pin that they are the
	// lengths a BLS key and signature actually have.
	require.Len(t, key, bls.PublicKeyLen)
	require.Len(t, proof, bls.SignatureLen)
}

// TestShippedConfigsParseTheirWholeSignerSet is the cost of checking the
// length here: a shipped config whose field does not fit its array stops
// building genesis at all, on every network that ships it.
//
// So it is checked, over every network GetConfig serves — the shipped stakers
// ARE the population FromConfig runs against, and the question "does the
// stricter rule refuse anybody?" has one answer per staker.
func TestShippedConfigsParseTheirWholeSignerSet(t *testing.T) {
	// SHIPPED is the whole claim, and GetConfig now has nothing else to
	// answer with: the shipped configs are compiled in, and no environment
	// variable or home directory reaches them (TestCanonicalConfigIsTheOnly-
	// Source). So this reads what the network ships on any box.
	//
	// The well-known networks and the chain-ID aliases GetConfig also answers
	// to, named by the package that SHIPS them.
	signed := 0
	for _, networkID := range []uint32{
		genesisconfigs.MainnetID, genesisconfigs.TestnetID, genesisconfigs.DevnetID, genesisconfigs.LocalID,
		genesisconfigs.MainnetChainID, genesisconfigs.TestnetChainID, genesisconfigs.DevnetChainID, genesisconfigs.LocalChainID,
	} {
		t.Run(fmt.Sprint(networkID), func(t *testing.T) {
			require := require.New(t)

			cfg, err := GetConfig(networkID)
			require.NoError(err)
			require.NotEmpty(cfg.InitialStakers,
				"network %d ships no stakers, so this case asserts nothing", networkID)

			for _, staker := range cfg.InitialStakers {
				require.NotNil(staker.Signer,
					"shipped staker %s declares no signer, so this case asserts nothing", staker.NodeID)

				got, err := parseProofOfPossession(staker.Signer)
				require.NoError(err,
					"the shipped config for network %d no longer builds: staker %s", networkID, staker.NodeID)
				require.NotNil(got)
				signed++
			}
		})
	}
	require.NotZero(t, signed, "no shipped config was read, so this test asserts nothing")
}

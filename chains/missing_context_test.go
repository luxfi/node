// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// missing_context_test.go — two decisions the chain manager makes about data it
// is handed, where the wrong answer is silent and total.
//
// A block whose parent we do not hold is not a bad block; it is a block we
// cannot place yet. Recognising that is what makes the node ask for the
// ancestry it is missing. Read as an ordinary failure instead, the block is
// discarded, no request goes out, nothing arrives to change the situation, and
// the node sits at its current height while peers keep gossiping blocks it
// keeps throwing away.
//
// The second decision is which chains get a config rewritten underneath them.

package chains

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// Every phrasing a VM uses for "I do not have the parent" must be recognised.
// Any one of them read as an ordinary failure is a node that stops advancing
// and never asks why.
func TestUnplaceableBlockIsRecognised(t *testing.T) {
	for _, msg := range []string{
		"unknown ancestor",
		"missing parent",
		"parent not found",
		"unknown parent",
		"missing context",
		"not found",
	} {
		t.Run(msg, func(t *testing.T) {
			require.True(t, isMissingContextError(errors.New(msg)),
				"the block is dropped instead of triggering an ancestry request, so the "+
					"node stops advancing while peers keep gossiping to it")

			// Real errors arrive wrapped, not bare.
			wrapped := errors.New("failed to verify block 0x1234: " + msg)
			require.True(t, isMissingContextError(wrapped),
				"recognised bare but not wrapped, and every real error is wrapped")
		})
	}
}

// The predicate must discriminate. Answering yes to everything turns every
// rejection — a bad signature, a malformed block — into an ancestry request to
// peers who have nothing we are missing.
func TestOrdinaryFailureIsNotMistakenForMissingContext(t *testing.T) {
	require.False(t, isMissingContextError(nil),
		"a block that verified is being treated as one we cannot place")

	for _, msg := range []string{
		"invalid signature",
		"block exceeds maximum size",
		"timestamp too far in the future",
		"insufficient funds",
	} {
		require.False(t, isMissingContextError(errors.New(msg)),
			"%q is treated as a missing parent, so the node answers a bad block by "+
				"requesting ancestry it already has", msg)
	}
}

// Automining rewrites a chain's config as JSON. Only the EVM reads its config
// that way; the chains that read a binary config refuse a JSON one outright and
// fail to start. So the rewrite must reach the EVM and nothing else — and must
// not happen at all when automining is off.
func TestConfigIsRewrittenOnlyForTheChainThatReadsJSON(t *testing.T) {
	require := require.New(t)

	original := []byte(`{"pruning-enabled":true}`)

	on := &manager{ManagerConfig: ManagerConfig{
		EnableAutomining: true,
		Log:              log.NewNoOpLogger(),
	}}

	evm := on.injectAutominingConfig(constants.EVMID, original)
	require.NotEqual(original, evm, "the EVM never received the automining config")
	require.Contains(string(evm), "enable-automining")

	// Any other chain is handed back exactly what it had.
	other := on.injectAutominingConfig(ids.GenerateTestID(), original)
	require.Equal(original, other,
		"a chain that reads a binary config was handed JSON, so it refuses the config and "+
			"never starts")

	// Automining off means no chain is rewritten, the EVM included.
	off := &manager{ManagerConfig: ManagerConfig{
		EnableAutomining: false,
		Log:              log.NewNoOpLogger(),
	}}
	require.Equal(original, off.injectAutominingConfig(constants.EVMID, original),
		"a node with automining off is mining anyway")
}

// The security profile pin is what a plugin compares against the profile it
// resolves for itself, so a binary running swapped profile content fails that
// compare. The pin must carry the hash — an ID alone names a profile without
// committing to its contents — and it must not be written into a chain that
// cannot read JSON.
func TestSecurityProfilePinCarriesTheHash(t *testing.T) {
	require := require.New(t)

	profile := &consensusconfig.ChainSecurityProfile{
		ProfileID:   0x01,
		ProfileName: "strict-pq",
		ProfileHash: [48]byte{0xde, 0xad, 0xbe, 0xef},
	}
	m := &manager{ManagerConfig: ManagerConfig{
		SecurityProfile: profile,
		Log:             log.NewNoOpLogger(),
	}}

	pinned := string(m.injectSecurityProfileConfig(constants.EVMID, []byte(`{"pruning-enabled":true}`)))
	require.Contains(pinned, "deadbeef",
		"the pin names a profile without committing to its contents, so a binary running "+
			"different content under the same ID passes the compare")
	require.Contains(pinned, "pruning-enabled",
		"the chain's own config was discarded by the pin")

	// A chain that reads a binary config is left alone.
	original := []byte(`{"pruning-enabled":true}`)
	require.Equal(original, m.injectSecurityProfileConfig(ids.GenerateTestID(), original))

	// With no profile resolved there is nothing to pin.
	none := &manager{ManagerConfig: ManagerConfig{Log: log.NewNoOpLogger()}}
	require.Equal(original, none.injectSecurityProfileConfig(constants.EVMID, original))
}

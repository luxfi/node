// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/node/vms/platformvm/genesis"
	pchaintxs "github.com/luxfi/node/vms/platformvm/txs"
)

// parsePolicy reads the operator form "3-of-5" into (K, N).
//
// The canonical parser is luxfi/threshold/pkg/quorum, but the node's genesis
// builder is a boot-critical path and does not take a dependency on a
// threshold-crypto library to read three integers. What this test pins is the
// FORM — that the genesis blob states a quorum in a spelling that cannot be
// confused with a polynomial degree — and the form is checkable here.
func parsePolicy(t *testing.T, s string) (k, n int) {
	t.Helper()
	_, err := fmt.Sscanf(s, "%d-of-%d", &k, &n)
	require.NoErrorf(t, err, "policy %q is not in the operator form \"3-of-5\"", s)
	require.Greaterf(t, k, 1, "policy %q: k=1 is not a threshold policy", s)
	require.LessOrEqualf(t, k, n, "policy %q can never be satisfied", s)
	return k, n
}

// canonicalMPCVMID is M-Chain's vmID in the encoding the node's plugin registry
// resolves against. Pinned literally: the plugin binary's FILENAME must equal
// it, and a change that leaves the two out of step produces a chain that is
// declared in genesis but that no node can start.
//
// The predecessor value, tGVBwRxpmD2aFdg3iYjgRvrCe8Jcmq9UNKxyHMus2NZ8WcD8t, is
// the retired `thresholdvm` identifier. It was what the Dockerfile installed
// the plugin under while genesis declared MPCVMID, so the two never met.
const canonicalMPCVMID = "qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS"

// M-Chain must be a GENESIS chain — in the P-Chain's chain set at height 0,
// tracked by every validator from boot — not a chain created later by a
// CreateChainTx someone has to remember to submit.
//
// The difference matters for custody specifically. A post-genesis chain exists
// only from the height its tx landed, so a node syncing from genesis has a
// window with no custody state, and the chain's very existence depends on an
// operator action that can be forgotten or fumbled. Bridged funds should not
// depend on that.
func TestMChainIsAGenesisChain(t *testing.T) {
	for _, networkID := range []uint32{
		constants.MainnetID,
		constants.TestnetID,
		constants.LocalID,
	} {
		cfg := GetConfig(networkID)
		require.NotNilf(t, cfg, "network %d has no genesis config", networkID)
		require.NotEmptyf(t, cfg.MChainGenesis,
			"network %d carries no mchain.json, so the builder skips M-Chain entirely", networkID)

		raw, _, err := FromConfig(cfg)
		require.NoErrorf(t, err, "network %d genesis build", networkID)

		parsed, err := genesis.Parse(raw)
		require.NoErrorf(t, err, "network %d genesis parse", networkID)

		// Genesis chains are CreateChainTx entries executed at height 0 — the
		// chain exists from the first block, without anyone submitting a tx.
		var found *pchaintxs.CreateChainTx
		for _, c := range parsed.Chains {
			u, ok := c.Unsigned.(*pchaintxs.CreateChainTx)
			require.True(t, ok, "a genesis chain entry must be a CreateChainTx")
			if u.VMID() == constants.MPCVMID {
				found = u
				break
			}
		}
		require.NotNilf(t, found,
			"network %d: no genesis chain with vmID %s (M-Chain)", networkID, constants.MPCVMID)
		require.Equal(t, "M-Chain", found.BlockchainName())
		require.Equalf(t, []byte(cfg.MChainGenesis), found.GenesisData(),
			"network %d: the VM must receive exactly the mchain.json bytes", networkID)
		require.Equalf(t, canonicalMPCVMID, found.VMID().String(),
			"network %d: genesis declares a vmID the plugin registry will look up by filename", networkID)
	}
}

// The genesis blob decides how many custodians must cooperate to move bridged
// funds, so it must state that in a form nothing can misread as a polynomial
// degree. A bare `"threshold": 3` reads as the signer count to an operator and
// as the degree to every threshold library, and those differ by one.
func TestMChainGenesisPolicyIsUnambiguousAndDeployable(t *testing.T) {
	for _, tc := range []struct {
		networkID uint32
		want      string
	}{
		{constants.MainnetID, "3-of-5"},
		{constants.TestnetID, "3-of-5"},
		{constants.LocalID, "2-of-3"},
	} {
		cfg := GetConfig(tc.networkID)
		require.NotNil(t, cfg)

		var blob struct {
			Policy string `json:"policy"`
			VM     string `json:"vm"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(cfg.MChainGenesis), &blob),
			"network %d mchain.json must decode, policy included", tc.networkID)

		require.Equalf(t, tc.want, blob.Policy, "network %d policy", tc.networkID)
		k, n := parsePolicy(t, blob.Policy)
		require.Greaterf(t, 2*k, n,
			"network %d: %s admits two disjoint quorums, so two halves of the committee could authorise contradictory releases",
			tc.networkID, blob.Policy)
		require.Equalf(t, "mpcvm", blob.VM,
			"network %d must name the current VM, not the retired ThresholdVM", tc.networkID)
	}
}

// The ambiguous fields must be gone, not merely ignored. Leaving them in place
// means the next reader has two numbers to choose between, and the whole point
// of the policy field is that there is exactly one.
func TestMChainGenesisHasNoAmbiguousThresholdFields(t *testing.T) {
	for _, networkID := range []uint32{constants.MainnetID, constants.TestnetID, constants.LocalID} {
		cfg := GetConfig(networkID)
		require.NotNil(t, cfg)

		var blob map[string]any
		require.NoError(t, json.Unmarshal([]byte(cfg.MChainGenesis), &blob))
		for _, dead := range []string{"mpcThreshold", "mpcParties", "threshold", "totalParties"} {
			require.NotContainsf(t, blob, dead,
				"network %d mchain.json still carries %q; the policy field is the only quorum statement",
				networkID, dead)
		}
	}
}

// A policy is only real if the network has enough validators to hold its
// shares. M-Chain draws its committee from the validator set, and RunKeygen
// refuses a policy needing more parties than the committee has — so a genesis
// declaring 7-of-10 on a five-validator network fails closed forever: no
// custody key can ever be generated, and the failure only shows up the first
// time someone tries to bridge.
//
// mainnet's mchain.json shipped exactly that mismatch for as long as the field
// existed, harmlessly, because nothing read it. This test is what keeps it
// harmless now that the policy is authoritative.
func TestMChainPolicyIsSatisfiableByTheGenesisValidatorSet(t *testing.T) {
	for _, networkID := range []uint32{constants.MainnetID, constants.TestnetID, constants.LocalID} {
		cfg := GetConfig(networkID)
		require.NotNil(t, cfg)

		raw, _, err := FromConfig(cfg)
		require.NoError(t, err)
		parsed, err := genesis.Parse(raw)
		require.NoError(t, err)

		var blob struct {
			Policy string `json:"policy"`
		}
		require.NoError(t, json.Unmarshal([]byte(cfg.MChainGenesis), &blob))
		_, n := parsePolicy(t, blob.Policy)

		require.LessOrEqualf(t, n, len(parsed.Validators),
			"network %d: M-Chain policy %s needs %d parties but genesis declares %d validators; "+
				"keygen would fail closed and no custody key could ever exist",
			networkID, blob.Policy, n, len(parsed.Validators))
	}
}

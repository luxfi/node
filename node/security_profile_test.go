// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
	genesisconfigs "github.com/luxfi/genesis/configs"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
	genesissecurity "github.com/luxfi/genesis/pkg/genesis/security"
	"github.com/luxfi/log"
	"github.com/luxfi/node/config/node"
)

// TestApplySecurityProfile_StrictPQ proves the locked-profile pin
// resolves end-to-end inside the node bootstrap path and the resulting
// *ChainSecurityProfile is reachable via SecurityProfile(). Closes the
// node-side half of red-team F102.
func TestApplySecurityProfile_StrictPQ(t *testing.T) {
	canonical := consensusconfig.StrictPQ()
	live, err := canonical.ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}

	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileStrictPQ),
		ProfileHashHex: hex.EncodeToString(live[:]),
	}
	n := &Node{Log: log.NoLog{}}

	if err := n.applySecurityProfile(pin); err != nil {
		t.Fatalf("applySecurityProfile returned: %v", err)
	}
	got := n.SecurityProfile()
	if got == nil {
		t.Fatal("SecurityProfile() returned nil after a successful pin resolution")
	}
	if got.ProfileID != uint32(consensusconfig.ProfileStrictPQ) {
		t.Errorf("SecurityProfile().ProfileID = %d; want %d",
			got.ProfileID, consensusconfig.ProfileStrictPQ)
	}
	if got.ProfileHash != live {
		t.Errorf("SecurityProfile().ProfileHash mismatch")
	}
	if !got.ProofPolicyID.IsPostQuantum() {
		t.Errorf("StrictPQ profile should be post-quantum")
	}
	if got.HashSuiteID != consensusconfig.HashSuiteSHA3NIST {
		t.Errorf("StrictPQ profile should pin SHA3NIST, got %s",
			got.HashSuiteID.String())
	}
}

// TestApplySecurityProfile_NilPin proves an absent pin is a logged
// warning, not a fatal error — legacy networks pre-locked-profile
// keep booting in classical-compat mode.
func TestApplySecurityProfile_NilPin(t *testing.T) {
	n := &Node{Log: log.NoLog{}}
	if err := n.applySecurityProfile(nil); err != nil {
		t.Fatalf("applySecurityProfile(nil) returned %v; want nil", err)
	}
	if n.SecurityProfile() != nil {
		t.Errorf("SecurityProfile() = %v; want nil", n.SecurityProfile())
	}
}

// TestApplySecurityProfile_HashMismatchRejected proves the node
// refuses to boot when the genesis-pinned hash diverges from the live
// canonical profile. This is the required F102 anti-regression:
// a forked binary cannot silently boot under a different profile by
// pinning the same ProfileID.
func TestApplySecurityProfile_HashMismatchRejected(t *testing.T) {
	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileStrictPQ),
		ProfileHashHex: strings.Repeat("00", 48),
	}
	n := &Node{Log: log.NoLog{}}
	err := n.applySecurityProfile(pin)
	if err == nil {
		t.Fatal("applySecurityProfile accepted a wrong-hash pin")
	}
	if !errors.Is(err, genesissecurity.ErrSecurityProfileHashMismatch) {
		t.Errorf("applySecurityProfile returned %v; want wrap of ErrSecurityProfileHashMismatch", err)
	}
	if n.SecurityProfile() != nil {
		t.Errorf("SecurityProfile() = %v; want nil after rejection", n.SecurityProfile())
	}
}

// TestApplySecurityProfile_UnknownProfileIDRejected proves an unknown
// ProfileID byte refuses to boot. This closes the "downstream chain
// spoofs a profile ID it didn't register" attack class.
func TestApplySecurityProfile_UnknownProfileIDRejected(t *testing.T) {
	pin := &genesiscfg.SecurityProfile{
		ProfileID:      0xFE, // unregistered
		ProfileHashHex: strings.Repeat("ab", 48),
	}
	n := &Node{Log: log.NoLog{}}
	err := n.applySecurityProfile(pin)
	if err == nil {
		t.Fatal("applySecurityProfile accepted an unknown ProfileID")
	}
	if !errors.Is(err, genesissecurity.ErrSecurityProfileInvalidID) {
		t.Errorf("applySecurityProfile returned %v; want wrap of ErrSecurityProfileInvalidID", err)
	}
}

// TestApplySecurityProfile_FIPS proves the FIPS profile resolves
// alongside StrictPQ — both share the strict-PQ posture but FIPS
// pins SHA3NIST exclusively (no BLAKE3-legacy).
func TestApplySecurityProfile_FIPS(t *testing.T) {
	canonical := consensusconfig.FIPS()
	live, err := canonical.ComputeHash()
	if err != nil {
		t.Fatalf("FIPS ComputeHash: %v", err)
	}
	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileFIPS),
		ProfileHashHex: hex.EncodeToString(live[:]),
	}
	n := &Node{Log: log.NoLog{}}
	if err := n.applySecurityProfile(pin); err != nil {
		t.Fatalf("applySecurityProfile(FIPS) returned: %v", err)
	}
	got := n.SecurityProfile()
	if got.ProfileID != uint32(consensusconfig.ProfileFIPS) {
		t.Errorf("ProfileID = %d; want %d", got.ProfileID, consensusconfig.ProfileFIPS)
	}
	if got.HashSuiteID != consensusconfig.HashSuiteSHA3NIST {
		t.Errorf("FIPS HashSuiteID = %s; want sha3-nist", got.HashSuiteID.String())
	}
}

// plantGenesisTree writes a complete genesis tree at [dir]: the shipped
// localnet config split into the files GetConfigFromDir reads, with the
// security-profile pin swapped for a VALID permissive one.
//
// Valid is the point. A pin whose hash does not match is refused as a bad pin,
// which is a different event from the one being tested — the interesting case
// is the downgrade that resolves cleanly and says nothing.
func plantGenesisTree(t *testing.T, dir string) {
	t.Helper()

	shipped, err := genesisconfigs.GetGenesisWithAllocations(genesisconfigs.LocalID, nil)
	if err != nil {
		t.Fatalf("reading the shipped config: %v", err)
	}
	var canonical map[string]any
	if err := json.Unmarshal(shipped, &canonical); err != nil {
		t.Fatalf("parsing the shipped config: %v", err)
	}

	permissive := consensusconfig.Permissive()
	live, err := permissive.ComputeHash()
	if err != nil {
		t.Fatalf("hashing the permissive profile: %v", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, v any) {
		t.Helper()
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("network.json", map[string]any{
		"networkID": canonical["networkID"],
		"startTime": canonical["startTime"],
		"message":   "planted",
	})
	write("pchain.json", map[string]any{
		"allocations":          canonical["allocations"],
		"initialStakeDuration": canonical["initialStakeDuration"],
		"initialStakedFunds":   canonical["initialStakedFunds"],
		"initialStakers":       canonical["initialStakers"],
	})
	write("cchain.json", canonical["cChainGenesis"])
	write("securityProfile.json", map[string]any{
		"profileID":      uint8(consensusconfig.ProfilePermissive),
		"profileHashHex": hex.EncodeToString(live[:]),
	})
}

// TestPlantedGenesisTreeCannotDowngradeTheProfile holds the chain's
// cryptography to the network that chose it.
//
// initSecurityProfile used to find the pin by sweeping ~/.lux/genesis,
// /etc/lux/genesis, /app/genesis and ~/work/lux/genesis/configs, and
// securityProfile.json is an optional shard in each of those trees. So
// whoever could write one chose the profile for the whole node, and choosing
// permissive turns off the post-quantum peer handshake, the strict-PQ mempool
// gate and the validator scheme check together — a downgrade that resolves
// cleanly and logs a normal-looking banner.
//
// The pin now comes from the config compiled into the binary, so both trees
// are read by nobody.
func TestPlantedGenesisTreeCannotDowngradeTheProfile(t *testing.T) {
	shipped := consensusconfig.StrictPQ()

	for _, tree := range []string{".lux/genesis", "work/lux/genesis/configs"} {
		t.Run(tree, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			plantGenesisTree(t, filepath.Join(home, tree, "localnet"))

			n := &Node{Log: log.NoLog{}, Config: &node.Config{}}
			n.Config.NetworkID = constants.LocalID

			if err := n.initSecurityProfile(); err != nil {
				t.Fatalf("initSecurityProfile returned: %v", err)
			}
			got := n.SecurityProfile()
			if got == nil {
				t.Fatal("a planted tree removed the profile pin")
			}
			if got.ProfileID != shipped.ProfileID {
				t.Errorf("profile is %s (id %d); the network pins %s (id %d) — a planted tree downgraded it",
					got.ProfileName, got.ProfileID, shipped.ProfileName, shipped.ProfileID)
			}
		})
	}
}

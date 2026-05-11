// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/log"
)

// TestApplySecurityProfile_LuxStrictPQ proves the locked-profile pin
// resolves end-to-end inside the node bootstrap path and the resulting
// *ChainSecurityProfile is reachable via SecurityProfile(). Closes the
// node-side half of red-team F102.
func TestApplySecurityProfile_LuxStrictPQ(t *testing.T) {
	canonical := consensusconfig.LuxStrictPQ()
	live, err := canonical.ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}

	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileLuxStrictPQ),
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
	if got.ProfileID != uint32(consensusconfig.ProfileLuxStrictPQ) {
		t.Errorf("SecurityProfile().ProfileID = %d; want %d",
			got.ProfileID, consensusconfig.ProfileLuxStrictPQ)
	}
	if got.ProfileHash != live {
		t.Errorf("SecurityProfile().ProfileHash mismatch")
	}
	if !got.ProofPolicyID.IsPostQuantum() {
		t.Errorf("LuxStrictPQ profile should be post-quantum")
	}
	if got.HashSuiteID != consensusconfig.HashSuiteSHA3NIST {
		t.Errorf("LuxStrictPQ profile should pin SHA3NIST, got %s",
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
// canonical profile. This is the load-bearing F102 anti-regression:
// a forked binary cannot silently boot under a different profile by
// pinning the same ProfileID.
func TestApplySecurityProfile_HashMismatchRejected(t *testing.T) {
	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileLuxStrictPQ),
		ProfileHashHex: strings.Repeat("00", 48),
	}
	n := &Node{Log: log.NoLog{}}
	err := n.applySecurityProfile(pin)
	if err == nil {
		t.Fatal("applySecurityProfile accepted a wrong-hash pin")
	}
	if !errors.Is(err, genesiscfg.ErrSecurityProfileHashMismatch) {
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
	if !errors.Is(err, genesiscfg.ErrSecurityProfileInvalidID) {
		t.Errorf("applySecurityProfile returned %v; want wrap of ErrSecurityProfileInvalidID", err)
	}
}

// TestApplySecurityProfile_LuxFIPS proves the FIPS profile resolves
// alongside StrictPQ — both share the strict-PQ posture but FIPS
// pins SHA3NIST exclusively (no BLAKE3-legacy).
func TestApplySecurityProfile_LuxFIPS(t *testing.T) {
	canonical := consensusconfig.LuxFIPS()
	live, err := canonical.ComputeHash()
	if err != nil {
		t.Fatalf("LuxFIPS ComputeHash: %v", err)
	}
	pin := &genesiscfg.SecurityProfile{
		ProfileID:      uint8(consensusconfig.ProfileLuxFIPS),
		ProfileHashHex: hex.EncodeToString(live[:]),
	}
	n := &Node{Log: log.NoLog{}}
	if err := n.applySecurityProfile(pin); err != nil {
		t.Fatalf("applySecurityProfile(LuxFIPS) returned: %v", err)
	}
	got := n.SecurityProfile()
	if got.ProfileID != uint32(consensusconfig.ProfileLuxFIPS) {
		t.Errorf("ProfileID = %d; want %d", got.ProfileID, consensusconfig.ProfileLuxFIPS)
	}
	if got.HashSuiteID != consensusconfig.HashSuiteSHA3NIST {
		t.Errorf("LuxFIPS HashSuiteID = %s; want sha3-nist", got.HashSuiteID.String())
	}
}

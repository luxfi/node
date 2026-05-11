// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// F118 regression coverage. Asserts the chain manager stamps the
// chain-wide ChainSecurityProfile pin into the C-Chain JSON config
// bytes so the rpcchainvm plugin (coreth) can resolve the profile on
// its side of the boundary.

package chains

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

// canonicalStrictPQProfile returns the live canonical StrictPQ profile
// with ProfileHash populated — the same form node.go builds during
// initSecurityProfile.
func canonicalStrictPQProfile(t *testing.T) *consensusconfig.ChainSecurityProfile {
	t.Helper()
	p, err := consensusconfig.ProfileByID(consensusconfig.ProfileStrictPQ)
	require.NoError(t, err)
	require.NoError(t, p.Validate())
	h, err := p.ComputeHash()
	require.NoError(t, err)
	p.ProfileHash = h
	return p
}

// TestInjectSecurityProfileConfig_StampsCChainOnly asserts the F118
// injection pass stamps the security profile pin into the C-Chain
// (EVMID) plugin config and is a no-op for every other VM ID. Other
// VMs receive the config bytes unchanged.
func TestInjectSecurityProfileConfig_StampsCChainOnly(t *testing.T) {
	require := require.New(t)
	profile := canonicalStrictPQProfile(t)

	m := &manager{
		ManagerConfig: ManagerConfig{
			Log:             log.NewNoOpLogger(),
			SecurityProfile: profile,
		},
	}

	// C-Chain (EVMID): pin must be stamped.
	out := m.injectSecurityProfileConfig(constants.EVMID, nil)
	require.NotEmpty(out, "C-Chain config must be stamped with the security profile pin")

	var got map[string]interface{}
	require.NoError(json.Unmarshal(out, &got))

	pin, ok := got["lux-security-profile"].(map[string]interface{})
	require.True(ok, "C-Chain config must carry lux-security-profile object")
	require.EqualValues(uint32(consensusconfig.ProfileStrictPQ), uint32(pin["profileID"].(float64)),
		"profileID must round-trip the canonical StrictPQ byte")
	require.Equal(hex.EncodeToString(profile.ProfileHash[:]), pin["profileHashHex"],
		"profileHashHex must round-trip the canonical 48-byte hash")

	// Non-EVM VM (PlatformVMID): no-op.
	original := []byte(`{"some": "config"}`)
	pOut := m.injectSecurityProfileConfig(constants.PlatformVMID, original)
	require.Equal(original, pOut, "non-C-Chain config must pass through unchanged")
}

// TestInjectSecurityProfileConfig_NoOpWhenProfileNil asserts the F118
// injection is a no-op when SecurityProfile is nil (classical-compat
// chains, legacy networks pre-locked-profile).
func TestInjectSecurityProfileConfig_NoOpWhenProfileNil(t *testing.T) {
	require := require.New(t)
	m := &manager{
		ManagerConfig: ManagerConfig{
			Log:             log.NewNoOpLogger(),
			SecurityProfile: nil,
		},
	}

	original := []byte(`{"some": "config"}`)
	out := m.injectSecurityProfileConfig(constants.EVMID, original)
	require.Equal(original, out, "no-op pass-through under nil SecurityProfile")

	out = m.injectSecurityProfileConfig(constants.EVMID, nil)
	require.Empty(out, "no-op pass-through preserves nil bytes")
}

// TestInjectSecurityProfileConfig_MergesExistingConfig asserts the F118
// injection preserves pre-existing C-Chain config keys (lux-strict-pq
// shim, enable-automining, etc.) while adding the new lux-security-profile
// pin alongside.
func TestInjectSecurityProfileConfig_MergesExistingConfig(t *testing.T) {
	require := require.New(t)
	profile := canonicalStrictPQProfile(t)
	m := &manager{
		ManagerConfig: ManagerConfig{
			Log:             log.NewNoOpLogger(),
			SecurityProfile: profile,
		},
	}

	original := []byte(`{"enable-automining": true, "skip-block-fee": true}`)
	out := m.injectSecurityProfileConfig(constants.EVMID, original)

	var got map[string]interface{}
	require.NoError(json.Unmarshal(out, &got))

	require.Equal(true, got["enable-automining"], "automining flag must survive")
	require.Equal(true, got["skip-block-fee"], "skip-block-fee flag must survive")
	require.NotNil(got["lux-security-profile"], "security profile pin must be added")
}

// TestInjectSecurityProfileConfig_RoundTripsAcrossPluginBoundary
// asserts the wire form coreth expects (security-profile pin with
// profileID + profileHashHex) matches the form the chain manager
// emits, so the rpcchainvm plugin can decode the pin via standard
// JSON unmarshalling. Closes the wire-compat axis of F118.
func TestInjectSecurityProfileConfig_RoundTripsAcrossPluginBoundary(t *testing.T) {
	require := require.New(t)
	profile := canonicalStrictPQProfile(t)
	m := &manager{
		ManagerConfig: ManagerConfig{
			Log:             log.NewNoOpLogger(),
			SecurityProfile: profile,
		},
	}

	out := m.injectSecurityProfileConfig(constants.EVMID, nil)

	// Mirror of plugin/evm/config.Config — locally redefined so this
	// test does not import the coreth plugin tree.
	type localPin struct {
		ProfileID      uint8  `json:"profileID"`
		ProfileHashHex string `json:"profileHashHex"`
	}
	type localCorethConfig struct {
		SecurityProfile *localPin `json:"lux-security-profile,omitempty"`
	}

	var cfg localCorethConfig
	require.NoError(json.Unmarshal(out, &cfg))
	require.NotNil(cfg.SecurityProfile, "coreth-side config struct must decode the pin")
	require.Equal(uint8(consensusconfig.ProfileStrictPQ), cfg.SecurityProfile.ProfileID)
	require.Equal(hex.EncodeToString(profile.ProfileHash[:]), cfg.SecurityProfile.ProfileHashHex)

	// Decoded hex must be exactly 48 bytes (SHA3-384 width).
	hashBytes, err := hex.DecodeString(cfg.SecurityProfile.ProfileHashHex)
	require.NoError(err)
	require.Len(hashBytes, 48, "wire pin must carry a 48-byte SHA3-384 ProfileHash")

	// Use ids.ID directly to silence the unused import warning in case
	// other tests in this file don't reach it.
	_ = ids.ID{}
}

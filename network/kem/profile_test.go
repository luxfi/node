// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLuxStrictE2EPQ_Banner asserts the canonical strict-PQ banner format
// matches the line-shape audit pipelines look for at boot.
func TestLuxStrictE2EPQ_Banner(t *testing.T) {
	require := require.New(t)

	p := LuxStrictE2EPQ()
	require.NoError(p.Validate())

	out := p.Banner()
	require.Contains(out, "SECURITY PROFILE: LUX_STRICT_E2E_PQ")
	require.Contains(out, "NODE IDENTITY:    ML-DSA-65")
	require.Contains(out, "SESSION KEM:      ML-KEM-768")
	require.Contains(out, "DKG KEM:          ML-KEM-1024")
	require.Contains(out, "HASH SUITE:       SHA3_NIST")
	require.Contains(out, "POST-QUANTUM E2E: true")

	// Banner is line-oriented; each axis is its own line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(lines, 6)
}

// TestActiveProfile_RefusesInternalInconsistency asserts a profile with
// drifted bytes is refused at Validate.
func TestActiveProfile_RefusesInternalInconsistency(t *testing.T) {
	require := require.New(t)

	bad := LuxStrictE2EPQ()
	bad.DKGKEM = KeyExchangeMLKEM768
	require.Error(bad.Validate())

	bad = LuxStrictE2EPQ()
	bad.SessionKEM = KeyExchangeX25519Unsafe
	require.Error(bad.Validate())

	bad = LuxStrictE2EPQ()
	bad.ForbidClassicalKEM = false
	require.Error(bad.Validate())

	bad = LuxStrictE2EPQ()
	bad.PostQuantumE2E = false
	require.Error(bad.Validate())

	bad = LuxStrictE2EPQ()
	bad.Name = ""
	require.Error(bad.Validate())
}
